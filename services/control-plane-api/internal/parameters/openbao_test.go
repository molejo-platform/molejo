package parameters

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenBaoKV2UsesKubernetesAuthAndExactVersions(t *testing.T) {
	t.Parallel()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("service-account-jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	logins := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/kubernetes/login":
			mu.Lock()
			logins++
			mu.Unlock()
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["role"] != "control-plane" || body["jwt"] != "service-account-jwt" {
				t.Fatalf("unexpected login body: %#v", body)
			}
			_, _ = w.Write([]byte(`{"auth":{"client_token":"opaque-token","lease_duration":300}}`))
		case r.URL.Path == "/v1/parameters/data/workspaces/ws-a/parameters/par-a" && r.Method == http.MethodPost:
			if r.Header.Get("X-Vault-Token") != "opaque-token" {
				t.Fatal("missing backend token")
			}
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int64 `json:"cas"`
				} `json:"options"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Data["value"] != "sensitive-value" || body.Options.CAS != 0 {
				t.Fatalf("unexpected write body: %#v", body)
			}
			_, _ = w.Write([]byte(`{"data":{"version":1}}`))
		case r.URL.Path == "/v1/parameters/data/workspaces/ws-a/parameters/par-a" && r.Method == http.MethodGet:
			if r.URL.Query().Get("version") != "1" {
				t.Fatalf("expected exact version, got %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":{"data":{"value":"sensitive-value"},"metadata":{"version":1}}}`))
		case r.URL.Path == "/v1/parameters/metadata/workspaces/ws-a/parameters/par-a" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":{"current_version":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewOpenBaoKV2(OpenBaoConfig{Address: server.URL, KubernetesRole: "control-plane", ServiceAccountTokenFile: tokenFile, Mount: "parameters", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.Put(context.Background(), "workspaces/ws-a/parameters/par-a", "sensitive-value", 0)
	if err != nil || version != 1 {
		t.Fatalf("Put() = %d, %v", version, err)
	}
	value, err := client.Get(context.Background(), "workspaces/ws-a/parameters/par-a", 1)
	if err != nil || value != "sensitive-value" {
		t.Fatalf("Get() = %q, %v", value, err)
	}
	currentVersion, err := client.CurrentVersion(context.Background(), "workspaces/ws-a/parameters/par-a")
	if err != nil || currentVersion != 1 {
		t.Fatalf("CurrentVersion() = %d, %v", currentVersion, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if logins != 1 {
		t.Fatalf("logins = %d, want 1", logins)
	}
}

func TestOpenBaoKV2MapsCASConflictWithoutLeakingResponse(t *testing.T) {
	t.Parallel()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/kubernetes/login" {
			_, _ = w.Write([]byte(`{"auth":{"client_token":"token","lease_duration":300}}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":["check-and-set parameter did not match current version: sensitive-value"]}`))
	}))
	defer server.Close()
	client, err := NewOpenBaoKV2(OpenBaoConfig{Address: server.URL, KubernetesRole: "control-plane", ServiceAccountTokenFile: tokenFile, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Put(context.Background(), "workspaces/ws-a/parameters/par-a", "sensitive-value", 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("Put() error = %v, want ErrConflict", err)
	}
}
