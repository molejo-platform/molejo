package store

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
)

func TestAgentEnrollmentIsSingleUseAndIdempotent(t *testing.T) {
	storage, _, actorID := newIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	tokenHash := auth.HashToken("enrollment-token")
	installation, err := storage.CreateAgentInstallation(t.Context(), newID(t, "agi"), "Lab cluster", tokenHash, now.Add(10*time.Minute), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "installation.agent.create", TargetType: "AgentInstallation", Outcome: audit.Succeeded})
	if err != nil {
		t.Fatal(err)
	}
	issued := AgentCertificate{CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), Serial: "01", Fingerprint: []byte("fingerprint"), NotAfter: now.Add(7 * 24 * time.Hour)}
	var signs atomic.Int32
	result, err := storage.EnrollAgent(t.Context(), tokenHash, "attempt-one", []byte("csr-one"), now, func(publicID string) (AgentCertificate, error) {
		signs.Add(1)
		if publicID != installation.PublicID {
			t.Fatalf("installation ID=%q", publicID)
		}
		return issued, nil
	}, audit.Event{PublicID: newID(t, "aud"), Action: "installation.agent.enroll", TargetType: "AgentInstallation", Outcome: audit.Succeeded})
	if err != nil {
		t.Fatal(err)
	}
	retried, err := storage.EnrollAgent(t.Context(), tokenHash, "attempt-one", []byte("csr-one"), now, func(string) (AgentCertificate, error) {
		signs.Add(1)
		return AgentCertificate{}, errors.New("must not sign twice")
	}, audit.Event{PublicID: newID(t, "aud"), Action: "installation.agent.enroll", TargetType: "AgentInstallation", Outcome: audit.Succeeded})
	if err != nil || !bytes.Equal(retried.CertificatePEM, result.CertificatePEM) || signs.Load() != 1 {
		t.Fatalf("retry=%+v signs=%d err=%v", retried, signs.Load(), err)
	}
	if _, err = storage.EnrollAgent(t.Context(), tokenHash, "attempt-two", []byte("csr-two"), now, func(string) (AgentCertificate, error) { return issued, nil }, audit.Event{}); !errors.Is(err, ErrAgentEnrollmentConsumed) {
		t.Fatalf("second enrollment error=%v", err)
	}
}

func TestBootstrapAgentInstallationIsIdempotent(t *testing.T) {
	storage, _, _ := newIntegrationFixture(t)
	ctx := t.Context()
	installationID := newID(t, "agi")
	firstHash := auth.HashToken("first-bootstrap-token")
	secondHash := auth.HashToken("second-bootstrap-token")
	if err := storage.EnsureBootstrapAgentInstallation(ctx, installationID, "Local cluster", firstHash, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := storage.EnsureBootstrapAgentInstallation(ctx, installationID, "Local cluster", secondHash, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var installations, tokens int
	var storedHash []byte
	if err := storage.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_installations WHERE public_id=$1`, installationID).Scan(&installations); err != nil {
		t.Fatal(err)
	}
	if err := storage.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_enrollment_tokens t JOIN agent_installations i ON i.id=t.installation_id WHERE i.public_id=$1`, installationID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if err := storage.Pool.QueryRow(ctx, `SELECT token_hash FROM agent_enrollment_tokens t JOIN agent_installations i ON i.id=t.installation_id WHERE i.public_id=$1`, installationID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if installations != 1 || tokens != 1 || !bytes.Equal(storedHash, secondHash) {
		t.Fatalf("installations=%d tokens=%d hash=%x", installations, tokens, storedHash)
	}
}

func TestAgentEnrollmentSerializesConcurrentConsumption(t *testing.T) {
	storage, _, actorID := newIntegrationFixture(t)
	now := time.Now().UTC()
	tokenHash := auth.HashToken("concurrent-token")
	if _, err := storage.CreateAgentInstallation(t.Context(), newID(t, "agi"), "Concurrent", tokenHash, now.Add(time.Minute), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "installation.agent.create", TargetType: "AgentInstallation", Outcome: audit.Succeeded}); err != nil {
		t.Fatal(err)
	}
	certificate := AgentCertificate{CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), Serial: "02", Fingerprint: []byte("fingerprint"), NotAfter: now.Add(time.Hour)}
	start := make(chan struct{})
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for _, attempt := range []string{"attempt-a", "attempt-b"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, enrollErr := storage.EnrollAgent(context.Background(), tokenHash, attempt, []byte(attempt), now, func(string) (AgentCertificate, error) { return certificate, nil }, audit.Event{PublicID: newID(t, "aud"), Action: "installation.agent.enroll", TargetType: "AgentInstallation", Outcome: audit.Succeeded})
			errorsFound <- enrollErr
		}()
	}
	close(start)
	workers.Wait()
	close(errorsFound)
	var succeeded, consumed int
	for result := range errorsFound {
		switch {
		case result == nil:
			succeeded++
		case errors.Is(result, ErrAgentEnrollmentConsumed):
			consumed++
		default:
			t.Fatalf("unexpected enrollment error: %v", result)
		}
	}
	if succeeded != 1 || consumed != 1 {
		t.Fatalf("succeeded=%d consumed=%d", succeeded, consumed)
	}
}

func TestAgentEnrollmentRejectsExpiredToken(t *testing.T) {
	storage, _, actorID := newIntegrationFixture(t)
	now := time.Now().UTC()
	tokenHash := auth.HashToken("expired-token")
	if _, err := storage.CreateAgentInstallation(t.Context(), newID(t, "agi"), "Expired token", tokenHash, now.Add(time.Minute), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "installation.agent.create", TargetType: "AgentInstallation", Outcome: audit.Succeeded}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.EnrollAgent(t.Context(), tokenHash, "attempt", []byte("csr"), now.Add(2*time.Minute), func(string) (AgentCertificate, error) { return AgentCertificate{}, nil }, audit.Event{}); !errors.Is(err, ErrAgentEnrollmentInvalid) {
		t.Fatalf("expired enrollment error=%v", err)
	}
}

func TestAgentActivationRejectsUnknownRevokedAndExpiredIdentities(t *testing.T) {
	storage, _, actorID := newIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	fingerprint := []byte("fingerprint")

	if _, err := storage.ActivateAgent(t.Context(), newID(t, "agi"), fingerprint, "cluster-test-uid", "v1.36.3", []string{"runtime.v1alpha1"}, now, audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("unknown installation error=%v", err)
	}

	createEnrolled := func(name string, notAfter time.Time) string {
		t.Helper()
		tokenHash := auth.HashToken(newID(t, "tok"))
		installation, err := storage.CreateAgentInstallation(t.Context(), newID(t, "agi"), name, tokenHash, now.Add(time.Minute), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "installation.agent.create", TargetType: "AgentInstallation", Outcome: audit.Succeeded})
		if err != nil {
			t.Fatal(err)
		}
		certificate := AgentCertificate{CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), Serial: newID(t, "ser"), Fingerprint: fingerprint, NotAfter: notAfter}
		if _, err = storage.EnrollAgent(t.Context(), tokenHash, newID(t, "ena"), []byte(name), now, func(string) (AgentCertificate, error) { return certificate, nil }, audit.Event{PublicID: newID(t, "aud"), Action: "installation.agent.enroll", TargetType: "AgentInstallation", Outcome: audit.Succeeded}); err != nil {
			t.Fatal(err)
		}
		return installation.PublicID
	}

	revokedID := createEnrolled("Revoked", now.Add(time.Hour))
	if _, err := storage.Pool.Exec(t.Context(), `UPDATE agent_installations SET status='Revoked' WHERE public_id=$1`, revokedID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ActivateAgent(t.Context(), revokedID, fingerprint, "cluster-test-uid", "v1.36.3", []string{"runtime.v1alpha1"}, now, audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("revoked installation error=%v", err)
	}

	expiredID := createEnrolled("Expired", now.Add(time.Hour))
	if _, err := storage.Pool.Exec(t.Context(), `UPDATE agent_installations SET certificate_not_after=$2 WHERE public_id=$1`, expiredID, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ActivateAgent(t.Context(), expiredID, fingerprint, "cluster-test-uid", "v1.36.3", []string{"runtime.v1alpha1"}, now, audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("expired certificate error=%v", err)
	}
}
