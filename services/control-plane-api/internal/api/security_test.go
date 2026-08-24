package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
)

func TestHandlerPropagatesARequestID(t *testing.T) {
	s := NewServer(nil, nil, DefaultConfig(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "client-request-123")
	recorder := httptest.NewRecorder()

	s.Handler().ServeHTTP(recorder, req)

	if got := recorder.Header().Get("X-Request-ID"); got != "client-request-123" {
		t.Fatalf("response request ID = %q, want client-request-123", got)
	}
}

func TestAcceptedOperationLogCorrelatesRequestAndOperation(t *testing.T) {
	var output bytes.Buffer
	server := NewServer(nil, nil, DefaultConfig(), slog.New(slog.NewJSONHandler(&output, nil)))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestIDContextKey{}, "request-123"))
	server.logAcceptedOperation(request, domain.Operation{PublicID: "op-123", DeploymentPublicID: "dep-123", Kind: "CreateDeployment"})

	log := output.String()
	for _, expected := range []string{`"request_id":"request-123"`, `"operation_id":"op-123"`, `"deployment_id":"dep-123"`, `"operation_kind":"CreateDeployment"`} {
		if !strings.Contains(log, expected) {
			t.Errorf("operation acceptance log is missing %s: %s", expected, log)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	s := &Server{Config: Config{AllowedOrigin: "https://console.example"}}
	request := httptest.NewRequest("POST", "/", nil)
	if !s.originAllowed(request) {
		t.Fatal("missing Origin should be allowed for same-origin requests")
	}
	request.Header.Set("Origin", "https://console.example")
	if !s.originAllowed(request) {
		t.Fatal("configured Origin should be allowed")
	}
	request.Header.Set("Origin", "https://evil.example")
	if s.originAllowed(request) {
		t.Fatal("unexpected Origin should be rejected")
	}
}

func TestCSRFValidation(t *testing.T) {
	s := &Server{Config: Config{AllowedOrigin: "https://console.example"}}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Origin", "https://console.example")
	token := "csrf-token"
	req.Header.Set("X-CSRF-Token", token)
	if !s.validCSRF(req, auth.HashToken(token)) {
		t.Fatal("expected CSRF token to verify")
	}
	req.Header.Set("X-CSRF-Token", "wrong")
	if s.validCSRF(req, auth.HashToken(token)) {
		t.Fatal("wrong CSRF token verified")
	}
}

func TestLoginLimiter(t *testing.T) {
	l := &loginLimiter{entries: map[string]loginAttempt{}}
	for i := 0; i < 8; i++ {
		if !l.allow("127.0.0.1") {
			t.Fatal("request was limited too early")
		}
		l.fail("127.0.0.1")
	}
	if l.allow("127.0.0.1") {
		t.Fatal("expected rate limit")
	}
	l.entries["127.0.0.1"] = loginAttempt{BlockedUntil: time.Now().Add(-time.Second)}
	l.success("127.0.0.1")
	if !l.allow("127.0.0.1") {
		t.Fatal("successful login should reset limiter")
	}
}
