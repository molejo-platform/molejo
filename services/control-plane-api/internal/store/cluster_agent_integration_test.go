package store

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/packages/workspacecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
)

const testTrustBundleID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestAgentEnrollmentIsSingleUseAndIdempotent(t *testing.T) {
	storage, _, actorID := newIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	tokenHash := auth.HashToken("enrollment-token")
	installation, err := storage.CreateAgentInstallation(t.Context(), newID(t, "agi"), "Lab cluster", tokenHash, now.Add(10*time.Minute), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "installation.agent.create", TargetType: "AgentInstallation", Outcome: audit.Succeeded})
	if err != nil {
		t.Fatal(err)
	}
	issued := AgentCertificate{CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), Serial: "01", Fingerprint: []byte("fingerprint"), TrustBundleID: testTrustBundleID, NotAfter: now.Add(7 * 24 * time.Hour)}
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
	certificate := AgentCertificate{CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), Serial: "02", Fingerprint: []byte("fingerprint"), TrustBundleID: testTrustBundleID, NotAfter: now.Add(time.Hour)}
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

	if _, err := storage.ActivateAgent(t.Context(), newID(t, "agi"), fingerprint, "cluster-test-uid", "test", "v1.36.3", []string{"runtime.v1alpha3"}, workspacecontract.ProvisioningNamespaced, testTrustBundleID, "ags-unknown", now, audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("unknown installation error=%v", err)
	}

	createEnrolled := func(name string, notAfter time.Time) (string, []byte) {
		t.Helper()
		tokenHash := auth.HashToken(newID(t, "tok"))
		installation, err := storage.CreateAgentInstallation(t.Context(), newID(t, "agi"), name, tokenHash, now.Add(time.Minute), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "installation.agent.create", TargetType: "AgentInstallation", Outcome: audit.Succeeded})
		if err != nil {
			t.Fatal(err)
		}
		credentialFingerprint := []byte("fingerprint-" + name)
		certificate := AgentCertificate{CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), Serial: newID(t, "ser"), Fingerprint: credentialFingerprint, TrustBundleID: testTrustBundleID, NotAfter: notAfter}
		if _, err = storage.EnrollAgent(t.Context(), tokenHash, newID(t, "ena"), []byte(name), now, func(string) (AgentCertificate, error) { return certificate, nil }, audit.Event{PublicID: newID(t, "aud"), Action: "installation.agent.enroll", TargetType: "AgentInstallation", Outcome: audit.Succeeded}); err != nil {
			t.Fatal(err)
		}
		return installation.PublicID, credentialFingerprint
	}

	revokedID, revokedFingerprint := createEnrolled("Revoked", now.Add(time.Hour))
	if _, err := storage.Pool.Exec(t.Context(), `UPDATE agent_installations SET status='Revoked' WHERE public_id=$1`, revokedID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ActivateAgent(t.Context(), revokedID, revokedFingerprint, "cluster-test-uid", "test", "v1.36.3", []string{"runtime.v1alpha3"}, workspacecontract.ProvisioningNamespaced, testTrustBundleID, "ags-revoked", now, audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("revoked installation error=%v", err)
	}

	expiredID, expiredFingerprint := createEnrolled("Expired", now.Add(time.Hour))
	if _, err := storage.Pool.Exec(t.Context(), `UPDATE agent_credentials SET certificate_not_after=$2
		WHERE installation_id=(SELECT id FROM agent_installations WHERE public_id=$1)`, expiredID, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ActivateAgent(t.Context(), expiredID, expiredFingerprint, "cluster-test-uid", "test", "v1.36.3", []string{"runtime.v1alpha3"}, workspacecontract.ProvisioningNamespaced, testTrustBundleID, "ags-expired", now, audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("expired certificate error=%v", err)
	}
}

func TestAgentCredentialRenewalIsIdempotentAndRevocable(t *testing.T) {
	storage, workspaceID, actorID := newIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	tokenHash := auth.HashToken("renewal-token")
	installation, err := storage.CreateAgentInstallation(t.Context(), newID(t, "cls"), "Renewal cluster", tokenHash, now.Add(time.Minute), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "cluster.create", TargetType: "Cluster", Outcome: audit.Succeeded})
	if err != nil {
		t.Fatal(err)
	}
	oldFingerprint := []byte("old-fingerprint")
	oldCertificate := AgentCertificate{CertificatePEM: []byte("old-certificate"), CACertificatePEM: []byte("identity-ca"), ServerCAPEM: []byte("server-ca"), Serial: "old-serial", Fingerprint: oldFingerprint, TrustBundleID: testTrustBundleID, NotAfter: now.Add(48 * time.Hour)}
	if _, err = storage.EnrollAgent(t.Context(), tokenHash, "enrollment-attempt", []byte("enrollment-csr"), now, func(string) (AgentCertificate, error) { return oldCertificate, nil }, audit.Event{PublicID: newID(t, "aud"), Action: "cluster.enroll", TargetType: "Cluster", Outcome: audit.Succeeded}); err != nil {
		t.Fatal(err)
	}
	const sessionID = "ags-renewal"
	if _, err = storage.ActivateAgent(t.Context(), installation.PublicID, oldFingerprint, "cluster-uid", "v0.1.0", "v1.36.3", []string{"runtime.v1alpha3"}, workspacecontract.ProvisioningNamespaced, testTrustBundleID, sessionID, now, audit.Event{PublicID: newID(t, "aud"), Action: "cluster.pair", TargetType: "Cluster", Outcome: audit.Succeeded}); err != nil {
		t.Fatal(err)
	}
	var workspaceNamespace string
	if err = storage.Pool.QueryRow(t.Context(), `SELECT namespace_name FROM workspaces WHERE id=$1`, workspaceID).Scan(&workspaceNamespace); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(t.Context(), `INSERT INTO workspace_clusters(workspace_id,installation_id,namespace_name,state,observed_generation)
		VALUES($1,$2,$3,'Ready',1)`, workspaceID, installation.ID, workspaceNamespace); err != nil {
		t.Fatal(err)
	}
	project, app, environment := createHierarchy(t, storage, workspaceID)
	privateConfig := integrationConfiguration("revocation-target")
	privateConfig.PublicEndpoints = nil
	target, _, err := storage.CreateAppEnvironmentOnCluster(t.Context(), workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, installation.PublicID, "main", "Stateless", privateConfig, nil)
	if err != nil {
		t.Fatal(err)
	}

	newFingerprint := []byte("new-fingerprint")
	newCertificate := AgentCertificate{CertificatePEM: []byte("new-certificate"), CACertificatePEM: []byte("identity-ca"), ServerCAPEM: []byte("server-ca"), Serial: "new-serial", Fingerprint: newFingerprint, TrustBundleID: testTrustBundleID, NotAfter: now.Add(7 * 24 * time.Hour)}
	var signs atomic.Int32
	renew := func() (AgentCertificate, error) {
		return storage.RenewAgent(t.Context(), installation.PublicID, oldFingerprint, "renewal-attempt", []byte("renewal-csr"), now, func(string) (AgentCertificate, error) {
			signs.Add(1)
			return newCertificate, nil
		}, audit.Event{PublicID: newID(t, "aud"), Action: "cluster.credential.renew", TargetType: "Cluster", Outcome: audit.Succeeded})
	}
	first, err := renew()
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := renew()
	if err != nil || signs.Load() != 1 || !bytes.Equal(first.CertificatePEM, replayed.CertificatePEM) {
		t.Fatalf("renewal replay=%+v signs=%d err=%v", replayed, signs.Load(), err)
	}
	if _, err = storage.RenewAgent(t.Context(), installation.PublicID, oldFingerprint, "unauthorized-renewal", []byte("unauthorized-csr"), now.Add(time.Minute), func(string) (AgentCertificate, error) {
		signs.Add(1)
		return AgentCertificate{}, errors.New("superseded credential must not sign")
	}, audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("superseded credential started a new renewal: %v", err)
	}
	if signs.Load() != 1 {
		t.Fatalf("superseded credential triggered signing: signs=%d", signs.Load())
	}
	if _, err = storage.ActivateAgent(t.Context(), installation.PublicID, oldFingerprint, "cluster-uid", "v0.1.0", "v1.36.3", []string{"runtime.v1alpha3"}, workspacecontract.ProvisioningNamespaced, testTrustBundleID, "ags-superseded", now.Add(time.Minute), audit.Event{}); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("superseded credential opened a new session: %v", err)
	}
	if err = storage.TouchAgent(t.Context(), installation.PublicID, oldFingerprint, sessionID, 1, now.Add(30*time.Minute)); err != nil {
		t.Fatalf("old credential should remain valid during overlap: %v", err)
	}
	if err = storage.TouchAgent(t.Context(), installation.PublicID, oldFingerprint, sessionID, 2, now.Add(2*time.Hour)); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("old credential after overlap error=%v", err)
	}
	if err = storage.TouchAgent(t.Context(), installation.PublicID, newFingerprint, sessionID, 2, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("new credential was rejected: %v", err)
	}
	if err = storage.TouchAgent(t.Context(), installation.PublicID, newFingerprint, sessionID, 2, now.Add(2*time.Hour)); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("replayed heartbeat sequence error=%v", err)
	}
	const replacementSessionID = "ags-replacement"
	if _, err = storage.ActivateAgent(t.Context(), installation.PublicID, newFingerprint, "cluster-uid", "v0.1.0", "v1.36.3", []string{"runtime.v1alpha3"}, workspacecontract.ProvisioningNamespaced, testTrustBundleID, replacementSessionID, now.Add(2*time.Hour), audit.Event{}); err != nil {
		t.Fatalf("replacement session activation: %v", err)
	}
	if err = storage.TouchAgent(t.Context(), installation.PublicID, newFingerprint, sessionID, 3, now.Add(2*time.Hour)); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("superseded session heartbeat error=%v", err)
	}
	if err = storage.TouchAgent(t.Context(), installation.PublicID, newFingerprint, replacementSessionID, 1, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("replacement session heartbeat: %v", err)
	}
	if err = storage.RevokeCluster(t.Context(), installation.PublicID, "operator requested revocation", now.Add(3*time.Hour), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "cluster.revoke", TargetType: "Cluster", Outcome: audit.Succeeded}); err != nil {
		t.Fatal(err)
	}
	if err = storage.RevokeCluster(t.Context(), installation.PublicID, "idempotent retry", now.Add(3*time.Hour), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "cluster.revoke", TargetType: "Cluster", Outcome: audit.Succeeded}); err != nil {
		t.Fatalf("idempotent revocation: %v", err)
	}
	var bindingState, bindingMessage, environmentState, environmentMessage string
	if err = storage.Pool.QueryRow(t.Context(), `SELECT state,message FROM workspace_clusters WHERE workspace_id=$1 AND installation_id=$2`, workspaceID, installation.ID).Scan(&bindingState, &bindingMessage); err != nil {
		t.Fatal(err)
	}
	if err = storage.Pool.QueryRow(t.Context(), `SELECT last_state,last_message FROM app_environments WHERE id=$1`, target.ID).Scan(&environmentState, &environmentMessage); err != nil {
		t.Fatal(err)
	}
	if bindingState != "Failed" || bindingMessage == "" || environmentState != "Unknown" || environmentMessage == "" {
		t.Fatalf("binding=%s/%q environment=%s/%q", bindingState, bindingMessage, environmentState, environmentMessage)
	}
	if err = storage.TouchAgent(t.Context(), installation.PublicID, newFingerprint, replacementSessionID, 2, now.Add(3*time.Hour)); !errors.Is(err, ErrAgentIdentityMismatch) {
		t.Fatalf("revoked credential error=%v", err)
	}

	replacementTokenHash := auth.HashToken("replacement-enrollment-token")
	replacement, err := storage.CreateAgentInstallation(t.Context(), newID(t, "cls"), "Replacement cluster", replacementTokenHash, now.Add(4*time.Hour), audit.Event{PublicID: newID(t, "aud"), ActorUserID: &actorID, Action: "cluster.create", TargetType: "Cluster", Outcome: audit.Succeeded})
	if err != nil {
		t.Fatal(err)
	}
	replacementFingerprint := []byte("replacement-fingerprint")
	replacementCertificate := AgentCertificate{CertificatePEM: []byte("replacement-certificate"), CACertificatePEM: []byte("identity-ca"), ServerCAPEM: []byte("server-ca"), Serial: "replacement-serial", Fingerprint: replacementFingerprint, TrustBundleID: testTrustBundleID, NotAfter: now.Add(7 * 24 * time.Hour)}
	if _, err = storage.EnrollAgent(t.Context(), replacementTokenHash, "replacement-enrollment", []byte("replacement-csr"), now.Add(3*time.Hour), func(string) (AgentCertificate, error) { return replacementCertificate, nil }, audit.Event{PublicID: newID(t, "aud"), Action: "cluster.enroll", TargetType: "Cluster", Outcome: audit.Succeeded}); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.ActivateAgent(t.Context(), replacement.PublicID, replacementFingerprint, "cluster-uid", "v0.1.0", "v1.36.3", []string{"runtime.v1alpha3"}, workspacecontract.ProvisioningNamespaced, testTrustBundleID, "ags-reenrolled", now.Add(3*time.Hour), audit.Event{PublicID: newID(t, "aud"), Action: "cluster.pair", TargetType: "Cluster", Outcome: audit.Succeeded}); err != nil {
		t.Fatalf("re-enroll revoked Kubernetes UID: %v", err)
	}
}
