package controlplane

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixedSecrets struct{ values [][]byte }

func (s *fixedSecrets) ReadSecret(string) ([]byte, error) {
	if len(s.values) == 0 {
		return nil, errors.New("unexpected secret prompt")
	}
	value := append([]byte(nil), s.values[0]...)
	s.values = s.values[1:]
	return value, nil
}

func TestEphemeralClientValidatesTLSOriginCookiesCSRFAndLogout(t *testing.T) {
	mutated, loggedOut := false, false
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session":
			if r.Header.Get("Origin") == "" {
				t.Fatal("login omitted Origin")
			}
			http.SetCookie(w, &http.Cookie{Name: "molejo_session", Value: "ephemeral", Path: "/", Secure: true})
			http.SetCookie(w, &http.Cookie{Name: "molejo_session_csrf", Value: "csrf", Path: "/", Secure: true})
			_, _ = w.Write([]byte(`{"csrfToken":"csrf","installationCapabilities":{"manageBindings":true},"workspaceMemberships":[],"user":{},"assuranceLevel":"AAL1"}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/grants/"):
			cookie, err := r.Cookie("molejo_session")
			if err != nil || cookie.Value != "ephemeral" || r.Header.Get("Origin") != server.URL || r.Header.Get("X-CSRF-Token") != "csrf" {
				t.Fatalf("mutation boundary missing: cookie=%v origin=%q csrf=%q", err, r.Header.Get("Origin"), r.Header.Get("X-CSRF-Token"))
			}
			mutated = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/session":
			loggedOut = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	caFile := writeServerCA(t, server)
	client, err := New(Config{Endpoint: server.URL, CAFile: caFile})
	if err != nil {
		t.Fatal(err)
	}
	secrets := &fixedSecrets{values: [][]byte{[]byte("correct horse")}}
	if err = client.Authenticate(t.Context(), "admin", secrets); err != nil {
		t.Fatal(err)
	}
	if err = client.PutPublicationGrant(t.Context(), "home", "ws-abcdefghijklmnopqrst", "binding"); err != nil {
		t.Fatal(err)
	}
	if err = client.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !mutated || !loggedOut {
		t.Fatalf("mutated=%v loggedOut=%v", mutated, loggedOut)
	}
}

func TestClientRejectsUnsafeEndpointAndCrossOriginRedirect(t *testing.T) {
	for _, endpoint := range []string{"http://control.example", "https://user:pass@control.example", "https://control.example/path", "https://control.example?token=x"} {
		if _, err := New(Config{Endpoint: endpoint}); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer source.Close()
	client, err := New(Config{Endpoint: source.URL, CAFile: writeServerCA(t, source)})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Authenticate(context.Background(), "admin", &fixedSecrets{values: [][]byte{[]byte("secret")}})
	if err == nil || !strings.Contains(err.Error(), "cross-origin redirect refused") {
		t.Fatalf("cross-origin redirect was not refused: %v", err)
	}
}

func TestEphemeralClientCompletesMFAWithoutPersistingSecrets(t *testing.T) {
	completed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"mfaRequired":true,"method":"TOTP","challengeToken":"challenge"}`))
		case "/api/v1/session/mfa/totp":
			completed = true
			http.SetCookie(w, &http.Cookie{Name: "molejo_session", Value: "mfa-session", Path: "/", Secure: true})
			http.SetCookie(w, &http.Cookie{Name: "molejo_session_csrf", Value: "csrf", Path: "/", Secure: true})
			_, _ = w.Write([]byte(`{"csrfToken":"csrf","installationCapabilities":{"manageBindings":true},"workspaceMemberships":[],"user":{},"assuranceLevel":"AAL2"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: server.URL, CAFile: writeServerCA(t, server)})
	if err != nil {
		t.Fatal(err)
	}
	secrets := &fixedSecrets{values: [][]byte{[]byte("password"), []byte("123456")}}
	if err = client.Authenticate(t.Context(), "admin", secrets); err != nil {
		t.Fatal(err)
	}
	if !completed || len(secrets.values) != 0 {
		t.Fatalf("MFA completion=%v unread secrets=%d", completed, len(secrets.values))
	}
}

func writeServerCA(t *testing.T, server *httptest.Server) string {
	t.Helper()
	certificate, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
