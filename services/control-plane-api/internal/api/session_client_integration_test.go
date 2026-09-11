package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
)

// Exercises the existing session contract as a CLI will: real TLS, a private
// cookie jar, password + MFA, CSRF/Origin, and explicit session revocation.
func TestEphemeralHTTPClientSessionWithMFA(t *testing.T) {
	storage, _, server, _ := newHierarchyAPITestFixture(t)
	password := "correct horse battery staple"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(t.Context(), `UPDATE password_credentials SET password_hash=$1`, hash); err != nil {
		t.Fatal(err)
	}
	var username string
	if err = storage.Pool.QueryRow(t.Context(), `SELECT username FROM users ORDER BY id LIMIT 1`).Scan(&username); err != nil {
		t.Fatal(err)
	}
	server.authenticationSecrets = &recordingSecretStore{values: map[string]string{}, versions: map[string]int64{}}
	server.passwordResetKey = []byte("01234567890123456789012345678901")
	server.config.TOTPEnabled = true
	endpoint := httptest.NewUnstartedServer(server.Handler())
	endpoint.StartTLS()
	defer endpoint.Close()
	parsed, _ := url.Parse(endpoint.URL)
	server.config.PublicURL = endpoint.URL
	server.config.AllowedOrigin = endpoint.URL
	server.config.AllowedHosts = []string{parsed.Host}
	client := endpoint.Client()
	client.Jar, _ = cookiejar.New(nil)
	csrf := ""
	request := func(method, path string, body any, origin, token string, want int) map[string]any {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), method, endpoint.URL+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", token)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s status=%d, expected=%d", method, path, response.StatusCode, want)
		}
		value := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &value)
		}
		return value
	}
	login := request("POST", "/api/v1/session", map[string]string{"username": username, "password": password}, endpoint.URL, "", 200)
	csrf, _ = login["csrfToken"].(string)
	if csrf == "" || len(client.Jar.Cookies(parsed)) == 0 {
		t.Fatal("login did not establish ephemeral session")
	}
	enrollment := request("POST", "/api/v1/users/me/mfa/totp/enrollment", map[string]string{"password": password}, endpoint.URL, csrf, 201)
	secret, _ := enrollment["secret"].(string)
	challenge, _ := enrollment["challengeToken"].(string)
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	request("PUT", "/api/v1/users/me/mfa/totp/enrollment", map[string]string{"challengeToken": challenge, "code": code}, endpoint.URL, csrf, 200)
	request("DELETE", "/api/v1/session", nil, "https://foreign.example", csrf, 403)
	request("DELETE", "/api/v1/session", nil, endpoint.URL, "wrong", 403)
	request("DELETE", "/api/v1/session", nil, endpoint.URL, csrf, 204)
	login = request("POST", "/api/v1/session", map[string]string{"username": username, "password": password}, endpoint.URL, "", 202)
	challenge, _ = login["challengeToken"].(string)
	code, err = totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	login = request("POST", "/api/v1/session/mfa/totp", map[string]string{"challengeToken": challenge, "code": code}, endpoint.URL, "", 200)
	if login["assuranceLevel"] != "AAL2" {
		t.Fatal("MFA did not establish AAL2")
	}
	csrf, _ = login["csrfToken"].(string)
	saved := append([]*http.Cookie(nil), client.Jar.Cookies(parsed)...)
	request("DELETE", "/api/v1/session", nil, endpoint.URL, csrf, 204)
	client.Jar.SetCookies(parsed, saved)
	request("GET", "/api/v1/session", nil, endpoint.URL, "", 401)
}
