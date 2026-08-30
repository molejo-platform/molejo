package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHandlerRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
		status     string
	}{
		{name: "root", method: http.MethodGet, path: "/", statusCode: http.StatusOK, status: "ok"},
		{name: "liveness", method: http.MethodGet, path: "/healthz", statusCode: http.StatusOK, status: "ok"},
		{name: "readiness", method: http.MethodGet, path: "/readyz", statusCode: http.StatusOK, status: "ok"},
		{name: "not ready", method: http.MethodGet, path: "/not-ready", statusCode: http.StatusServiceUnavailable, status: "not-ready"},
		{name: "unknown", method: http.MethodGet, path: "/unknown", statusCode: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodPost, path: "/readyz", statusCode: http.StatusMethodNotAllowed},
	}

	handler := newHandler()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			responseRecorder := httptest.NewRecorder()

			handler.ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != test.statusCode {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, test.statusCode)
			}
			if test.status == "" {
				return
			}
			var payload response
			if err := json.NewDecoder(responseRecorder.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Status != test.status {
				t.Fatalf("status = %q, want %q", payload.Status, test.status)
			}
		})
	}
}

func TestGraphQLTransport(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ status version }"}`))
	request.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", responseRecorder.Code, http.StatusOK, responseRecorder.Body.String())
	}
	var payload struct {
		Data response `json:"data"`
	}
	if err := json.NewDecoder(responseRecorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode GraphQL response: %v", err)
	}
	if payload.Data.Status != "ok" || payload.Data.Version != version {
		t.Fatalf("unexpected GraphQL payload: %#v", payload)
	}
}

func TestPersistentMarker(t *testing.T) {
	t.Setenv("FIXTURE_STATE_FILE", filepath.Join(t.TempDir(), "marker"))
	handler := newHandler()

	writeRequest := httptest.NewRequest(http.MethodPut, "/state", strings.NewReader("stateful-marker"))
	writeResponse := httptest.NewRecorder()
	handler.ServeHTTP(writeResponse, writeRequest)
	if writeResponse.Code != http.StatusNoContent {
		t.Fatalf("write status = %d, want %d: %s", writeResponse.Code, http.StatusNoContent, writeResponse.Body.String())
	}

	readRequest := httptest.NewRequest(http.MethodGet, "/state", nil)
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK || strings.TrimSpace(readResponse.Body.String()) != "stateful-marker" {
		t.Fatalf("read status = %d body = %q", readResponse.Code, readResponse.Body.String())
	}
}

func TestSSETransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	responseRecorder := httptest.NewRecorder()

	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	if got := responseRecorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	body := responseRecorder.Body.String()
	if !strings.Contains(body, "event: status\n") || !strings.Contains(body, `"version":"`+version+`"`) {
		t.Fatalf("unexpected SSE body: %q", body)
	}
}

func TestSSETransportRemainsOpen(t *testing.T) {
	server := httptest.NewServer(newHandler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("create SSE request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}

	events := make(chan struct{}, 16)
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			if scanner.Text() != "event: status" {
				continue
			}
			select {
			case events <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
		done <- scanner.Err()
	}()

	select {
	case <-events:
	case err := <-done:
		t.Fatalf("SSE stream ended before its first event: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the first SSE event")
	}

	const minimumPersistentLifetime = 250 * time.Millisecond
	select {
	case err := <-done:
		t.Fatalf("SSE stream ended before %s: %v", minimumPersistentLifetime, err)
	case <-time.After(minimumPersistentLifetime):
	}
}

func TestWebSocketTransport(t *testing.T) {
	server := httptest.NewServer(newHandler())
	t.Cleanup(server.Close)
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	connection, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	for index, message := range []string{"before-idle", "after-idle"} {
		if index > 0 {
			time.Sleep(250 * time.Millisecond)
		}
		_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := connection.WriteJSON(map[string]string{"message": message}); err != nil {
			t.Fatalf("write WebSocket message %q: %v", message, err)
		}
		_, body, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read WebSocket message %q: %v", message, err)
		}
		if strings.TrimSpace(string(body)) != `{"message":"`+message+`","version":"`+version+`"}` {
			t.Fatalf("unexpected WebSocket response after message %q: %s", message, body)
		}
	}
}

func TestTransportMethodsAndPayloadLimits(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       io.Reader
		statusCode int
	}{
		{name: "GraphQL requires POST", method: http.MethodGet, path: "/graphql", statusCode: http.StatusMethodNotAllowed},
		{name: "SSE requires GET", method: http.MethodPost, path: "/events", statusCode: http.StatusMethodNotAllowed},
		{name: "GraphQL rejects an oversized body", method: http.MethodPost, path: "/graphql",
			body:       strings.NewReader(`{"query":"` + strings.Repeat("a", maxGraphQLBodyBytes) + `"}`),
			statusCode: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, test.body)
			responseRecorder := httptest.NewRecorder()
			newHandler().ServeHTTP(responseRecorder, request)
			if responseRecorder.Code != test.statusCode {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, test.statusCode)
			}
		})
	}
}

func TestOutbound(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("controlled-upstream"))
	}))
	t.Cleanup(upstream.Close)

	request := httptest.NewRequest(http.MethodGet, "/outbound?url="+url.QueryEscape(upstream.URL), nil)
	responseRecorder := httptest.NewRecorder()
	newHandler().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d: %s", responseRecorder.Code, http.StatusOK, responseRecorder.Body.String())
	}
	var payload response
	if err := json.NewDecoder(responseRecorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.UpstreamStatus != http.StatusOK || payload.UpstreamBody != "controlled-upstream" {
		t.Fatalf("unexpected upstream response: %#v", payload)
	}
}

func TestOutboundRejectsInvalidURL(t *testing.T) {
	for _, rawURL := range []string{"", "relative", "file:///etc/passwd"} {
		t.Run(strings.ReplaceAll(rawURL, "/", "_"), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/outbound?url="+url.QueryEscape(rawURL), nil)
			responseRecorder := httptest.NewRecorder()

			newHandler().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != http.StatusBadRequest {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusBadRequest)
			}
		})
	}
}
