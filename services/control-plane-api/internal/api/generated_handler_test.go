package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeneratedChiRouterValidatesContractParametersBeforeTheHandler(t *testing.T) {
	server := NewServer(nil, nil, DefaultConfig(), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces?limit=abc", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("generated parameter validation response = %d %s", recorder.Code, recorder.Body.String())
	}
}
