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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHandlerPropagatesARequestID(t *testing.T) {
	s := NewServer(nil, nil, DefaultConfig(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Host = "127.0.0.1:8080"
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
	server.logAcceptedOperation(request, domain.Operation{PublicID: "op-123", DeploymentPublicID: "ap-123", Kind: "CreateDeployment"})

	log := output.String()
	for _, expected := range []string{`"request_id":"request-123"`, `"operation_id":"op-123"`, `"deployment_id":"ap-123"`, `"operation_kind":"CreateDeployment"`} {
		if !strings.Contains(log, expected) {
			t.Errorf("operation acceptance log is missing %s: %s", expected, log)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	s := &Server{Config: Config{AllowedOrigin: "https://console.example"}}
	request := httptest.NewRequest("POST", "/", nil)
	if s.originAllowed(request) {
		t.Fatal("sensitive request without Origin should be rejected")
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

func TestSecurityMiddlewareRejectsUnknownHostsAndUntrustedForwardedTLS(t *testing.T) {
	config := DefaultConfig()
	server := NewServer(nil, nil, config, nil)

	unknown := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	unknown.Host = "evil.example"
	unknownRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(unknownRecorder, unknown)
	if unknownRecorder.Code != http.StatusMisdirectedRequest {
		t.Fatalf("unknown host status = %d", unknownRecorder.Code)
	}

	untrusted := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	untrusted.Host = "127.0.0.1:8080"
	untrusted.RemoteAddr = "203.0.113.10:1234"
	untrusted.Header.Set("X-Forwarded-Proto", "https")
	untrustedRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(untrustedRecorder, untrusted)
	if got := untrustedRecorder.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("untrusted forwarded TLS emitted HSTS %q", got)
	}
}

func TestSecurityMiddlewareTrustsForwardedTLSOnlyFromConfiguredCIDRs(t *testing.T) {
	config := DefaultConfig()
	config.PublicURL = "https://127.0.0.1:8080"
	config.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	server := NewServer(nil, nil, config, nil)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Host = "127.0.0.1:8080"
	request.RemoteAddr = "10.2.3.4:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("trusted forwarded TLS did not emit HSTS")
	}
}

func TestHTTPTraceContainsOnlySanitizedRequestMetadata(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	server := NewServer(nil, nil, DefaultConfig(), nil)
	server.Tracer = provider.Tracer("test")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployments/ap-secret-value", strings.NewReader(`{"password":"do-not-record","csrf":"do-not-record"}`))
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count = %d", len(spans))
	}
	serialized := spans[0].Name
	for _, item := range spans[0].Attributes {
		serialized += string(item.Key) + item.Value.AsString()
	}
	for _, forbidden := range []string{"ap-secret-value", "do-not-record", "password", "csrf"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("trace contains sensitive value %q: %s", forbidden, serialized)
		}
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

func TestIdempotencyPayloadIncludesTheMutationPath(t *testing.T) {
	first := httptest.NewRequest(http.MethodPost, "/api/v1/apps/app-a/builds", strings.NewReader(`{}`))
	first.Header.Set("Idempotency-Key", "same-key")
	second := httptest.NewRequest(http.MethodPost, "/api/v1/apps/app-b/builds", strings.NewReader(`{}`))
	second.Header.Set("Idempotency-Key", "same-key")

	_, firstBodyHash, firstOK := idempotency(first)
	_, secondBodyHash, secondOK := idempotency(second)
	firstHash := scopedBuildPayloadHash(first, firstBodyHash)
	secondHash := scopedBuildPayloadHash(second, secondBodyHash)
	if !firstOK || !secondOK || bytes.Equal(firstHash, secondHash) {
		t.Fatal("the same key and body on different build resources were treated as the same mutation")
	}
}

func TestSessionEndpointsRejectInvalidOriginAndDisableCaching(t *testing.T) {
	server := NewServer(nil, nil, Config{
		CookieName:    "fruto_session",
		AllowedOrigin: "https://console.example",
		AllowedHosts:  []string{"console.example"},
	}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"actor":"owner","password":"secret"}`))
	request.Host = "console.example"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://invalid.example")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("invalid login origin status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("session Cache-Control = %q, want no-store", got)
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
