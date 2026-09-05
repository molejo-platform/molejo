package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestMockConsole(t *testing.T) {
	t.Parallel()
	handler, err := newHandler("", nil)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/healthz", want: "ok"},
		{path: "/", want: "Molejo Console"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("GET %s status=%d body=%q", test.path, response.Code, response.Body.String())
		}
	}
}

func TestAPIProxyPreservesControlPlanePath(t *testing.T) {
	t.Parallel()
	var upstreamPath string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		upstreamPath = request.URL.Path
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	handler, err := newHandler("https://api.example.test", transport)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("GET /api/v1/session status=%d body=%q", response.Code, response.Body.String())
	}
	if upstreamPath != "/api/v1/session" {
		t.Fatalf("upstream path=%q, want /api/v1/session", upstreamPath)
	}
}
