package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
)

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
