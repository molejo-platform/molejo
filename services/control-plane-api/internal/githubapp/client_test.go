package githubapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClientResolvesAndDownloadsOnlyAnExactCommit(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	var archive bytes.Buffer
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			return jsonResponse(http.StatusCreated, `{"token":"ephemeral-installation-token"}`), nil
		case "/repositories/99/commits/main":
			return jsonResponse(http.StatusOK, `{"sha":"`+sha+`","commit":{"message":"Ship the webhook\n\nDetails","author":{"name":"Molejo Bot","date":"2026-08-29T12:00:00Z"}},"author":{"login":"molejo-bot"}}`), nil
		case "/repositories/99/tarball/" + sha:
			if r.Header.Get("Authorization") != "Bearer ephemeral-installation-token" {
				t.Fatalf("archive authorization=%q", r.Header.Get("Authorization"))
			}
			response := jsonResponse(http.StatusOK, "archive-bytes")
			response.Header.Set("Content-Type", "application/x-gzip")
			return response, nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}
	client := newTestClient(t, "https://api.github.test", time.Now())
	client.httpClient = httpClient
	metadata, err := client.Commit(context.Background(), 42, 99, "main")
	if err != nil || metadata.SHA != sha || metadata.Title != "Ship the webhook" || metadata.AuthorName != "Molejo Bot" || metadata.AuthorLogin != "molejo-bot" || metadata.CommittedAt == nil {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	if err = client.Archive(context.Background(), 42, 99, metadata.SHA, &archive); err != nil {
		t.Fatal(err)
	}
	if archive.String() != "archive-bytes" {
		t.Fatalf("archive=%q", archive.String())
	}
}

func TestClientUsesShortLivedAppJWTAndDoesNotPersistInstallationTokens(t *testing.T) {
	fixed := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	var appAuthorization, installationAuthorization string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			appAuthorization = r.Header.Get("Authorization")
			return jsonResponse(http.StatusCreated, `{"token":"ephemeral-installation-token"}`), nil
		case "/installation/repositories":
			installationAuthorization = r.Header.Get("Authorization")
			return jsonResponse(http.StatusOK, `{"repositories":[{"id":99,"name":"api","full_name":"molejo/api","private":true,"default_branch":"main"}]}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}

	client := newTestClient(t, "https://api.github.test", fixed)
	client.httpClient = httpClient
	repositories, err := client.Repositories(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].ID != "99" || repositories[0].FullName != "molejo/api" {
		t.Fatalf("repositories=%+v", repositories)
	}
	if installationAuthorization != "Bearer ephemeral-installation-token" {
		t.Fatalf("installation authorization=%q", installationAuthorization)
	}
	parts := strings.Split(strings.TrimPrefix(appAuthorization, "Bearer "), ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT authorization=%q", appAuthorization)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		IssuedAt int64  `json:"iat"`
		Expires  int64  `json:"exp"`
		Issuer   string `json:"iss"`
	}
	if err = json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "123" || claims.IssuedAt != fixed.Add(-time.Minute).Unix() || claims.Expires != fixed.Add(9*time.Minute).Unix() {
		t.Fatalf("claims=%+v", claims)
	}
}

func TestClientRequiresUserAccessToThePendingInstallation(t *testing.T) {
	revocations := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			return jsonResponse(http.StatusOK, `{"access_token":"user-token"}`), nil
		case "/user/installations":
			if r.Header.Get("Authorization") != "Bearer user-token" {
				t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"installations":[{"id":41},{"id":42}]}`), nil
		case "/applications/client-id/token":
			revocations++
			if username, password, ok := r.BasicAuth(); !ok || username != "client-id" || password != "client-secret" {
				t.Fatalf("invalid revocation credentials")
			}
			return jsonResponse(http.StatusNoContent, ""), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}

	client := newTestClient(t, "https://api.github.test", time.Now())
	client.webBaseURL = "https://github.test"
	client.httpClient = httpClient
	ok, err := client.UserCanAccessInstallation(context.Background(), "one-time-code", 42)
	if err != nil || !ok {
		t.Fatalf("authorized=%v err=%v", ok, err)
	}
	ok, err = client.UserCanAccessInstallation(context.Background(), "one-time-code", 43)
	if err != nil || ok {
		t.Fatalf("authorized=%v err=%v", ok, err)
	}
	if revocations != 2 {
		t.Fatalf("revocations=%d", revocations)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestAuthorizationURLsCarryOnlyOpaqueState(t *testing.T) {
	client := newTestClient(t, "https://api.github.test", time.Now())
	if got := client.InstallationURL("opaque state"); got != "https://github.com/apps/molejo-test/installations/new?state=opaque+state" {
		t.Fatalf("installation URL=%q", got)
	}
	if got := client.UserAuthorizationURL("opaque state"); !strings.Contains(got, "client_id=client-id") || !strings.Contains(got, "redirect_uri=https%3A%2F%2Fcloud.molejo.dev%2Fapi%2Fv1%2Fgithub%2Fcallback") || !strings.Contains(got, "state=opaque+state") {
		t.Fatalf("authorization URL=%q", got)
	}
}

func newTestClient(t *testing.T, baseURL string, now time.Time) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	client, err := New(Config{AppID: 123, Slug: "molejo-test", ClientID: "client-id", ClientSecret: "client-secret", PrivateKey: privateKey, CallbackURL: "https://cloud.molejo.dev/api/v1/github/callback", APIBaseURL: baseURL, HTTPClient: http.DefaultClient, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
