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

func TestGeneratedChiRouterKeepsAppEnvironmentDeletionOnTheResourcePath(t *testing.T) {
	server := NewServer(nil, nil, DefaultConfig(), nil)
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/ws-aaaaaaaaaaaaaaaaaaaa/projects/prj-aaaaaaaaaaaaaaaaaaaa/apps/app-aaaaaaaaaaaaaaaaaaaa/environments/aev-aaaaaaaaaaaaaaaaaaaa", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Idempotency-Key", "router-delete")
	request.Header.Set("If-Match", "1")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("App Environment DELETE routing response = %d %s", recorder.Code, recorder.Body.String())
	}
}
