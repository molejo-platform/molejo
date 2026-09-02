package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
