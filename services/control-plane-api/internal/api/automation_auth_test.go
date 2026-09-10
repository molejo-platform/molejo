package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func TestAutomationBearerToken(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, test := range []struct {
		name   string
		header string
		valid  bool
	}{
		{name: "bearer", header: "Bearer " + token, valid: true},
		{name: "case insensitive scheme", header: "bearer " + token, valid: true},
		{name: "missing", valid: false},
		{name: "wrong scheme", header: "Basic " + token, valid: false},
		{name: "short", header: "Bearer short", valid: false},
		{name: "extra field", header: "Bearer " + token + " extra", valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, valid := automationBearerToken(test.header)
			if valid != test.valid || valid && value != token {
				t.Fatalf("automationBearerToken() = (%q,%v), want valid=%v", value, valid, test.valid)
			}
		})
	}
}

func TestWriteAutomationErrorPreservesAuthenticationAndStorageFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code int
	}{
		{name: "invalid credential", err: store.ErrAutomationAuthentication, code: http.StatusUnauthorized},
		{name: "name conflict", err: store.ErrNameConflict, code: http.StatusConflict},
		{name: "idempotency conflict", err: store.ErrIdempotencyConflict, code: http.StatusConflict},
		{name: "storage failure", err: errors.New("database unavailable"), code: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeAutomationError(response, httptest.NewRequest(http.MethodGet, "/", nil), test.err)
			if response.Code != test.code {
				t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), test.code)
			}
		})
	}
}
