package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeneratedChiRouterValidatesContractParametersBeforeTheHandler(t *testing.T) {
	server := NewServer(nil, nil, DefaultConfig(), nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", strings.NewReader(`{}`))
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("generated parameter validation response = %d %s", recorder.Code, recorder.Body.String())
	}
}
