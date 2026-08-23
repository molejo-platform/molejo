package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
