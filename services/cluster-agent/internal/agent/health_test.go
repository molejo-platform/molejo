package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthSeparatesProcessHealthFromPairingState(t *testing.T) {
	status := NewStatus()
	handler := HealthHandler(status)
	request := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	if response := request("/healthz"); response.Code != http.StatusOK {
		t.Fatalf("liveness=%d", response.Code)
	}
	if response := request("/readyz"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("initial readiness=%d", response.Code)
	}
	status.Set(StateUnpaired, "waiting for an enrollment token")
	if response := request("/readyz"); response.Code != http.StatusOK {
		t.Fatalf("unpaired readiness=%d body=%s", response.Code, response.Body.String())
	}
	if response := request("/status"); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"Unpaired"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
