package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubSignatureRequiresTheExactBodyAndSHA256Secret(t *testing.T) {
	secret := bytes.Repeat([]byte("s"), 32)
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	signature := githubSignature(body, secret)
	if !validGitHubSignature(body, signature, secret) {
		t.Fatal("valid GitHub signature was rejected")
	}
	if validGitHubSignature(append(append([]byte{}, body...), ' '), signature, secret) {
		t.Fatal("signature accepted a modified payload")
	}
	if validGitHubSignature(body, signature, bytes.Repeat([]byte("x"), 32)) {
		t.Fatal("signature accepted a different secret")
	}
	if validGitHubSignature(body, "sha1=invalid", secret) {
		t.Fatal("legacy signature algorithm was accepted")
	}
}

func TestGitHubWebhookPersistsOnceAndRejectsDeliveryIDReuse(t *testing.T) {
	storage, _, _, _ := newExecutorIntegrationFixture(t)
	secret := bytes.Repeat([]byte("s"), 32)
	server := NewServer(storage, nil, DefaultConfig(), nil)
	server.GitHubWebhookSecret = secret
	body := []byte(`{"zen":"Keep it logically awesome."}`)

	first := webhookRequest(server, secret, "delivery-idempotent", "ping", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first webhook status=%d body=%s", first.Code, first.Body.String())
	}
	duplicate := webhookRequest(server, secret, "delivery-idempotent", "ping", body)
	if duplicate.Code != http.StatusOK || !bytes.Contains(duplicate.Body.Bytes(), []byte(`"duplicate":true`)) {
		t.Fatalf("duplicate webhook status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	different := webhookRequest(server, secret, "delivery-idempotent", "ping", []byte(`{"zen":"different"}`))
	if different.Code != http.StatusConflict {
		t.Fatalf("reused delivery ID status=%d body=%s", different.Code, different.Body.String())
	}
	unsigned := webhookRequest(server, bytes.Repeat([]byte("x"), 32), "delivery-unsigned", "ping", body)
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status=%d body=%s", unsigned.Code, unsigned.Body.String())
	}
}

func TestPrereleaseWebhookIsPersistedAsIgnored(t *testing.T) {
	storage, _, _, _ := newExecutorIntegrationFixture(t)
	secret := bytes.Repeat([]byte("s"), 32)
	server := NewServer(storage, nil, DefaultConfig(), nil)
	server.GitHubWebhookSecret = secret
	body := []byte(`{"action":"published","installation":{"id":42},"repository":{"id":99,"full_name":"molejo/platform"},"release":{"tag_name":"v1.0.0-rc.1","target_commitish":"main","prerelease":true}}`)
	response := webhookRequest(server, secret, "delivery-prerelease", "release", body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("prerelease webhook status=%d body=%s", response.Code, response.Body.String())
	}
	var action string
	if err := storage.Pool.QueryRow(context.Background(), `SELECT action FROM github_deliveries WHERE delivery_id='delivery-prerelease'`).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "ignored" {
		t.Fatalf("prerelease action=%q, want ignored", action)
	}
}

func webhookRequest(server *Server, secret []byte, deliveryID, event string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/github/webhooks", bytes.NewReader(body))
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Delivery", deliveryID)
	request.Header.Set("X-GitHub-Event", event)
	request.Header.Set("X-Hub-Signature-256", githubSignature(body, secret))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func githubSignature(body, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
