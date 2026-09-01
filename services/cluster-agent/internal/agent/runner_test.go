package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

type memoryIdentityStore struct {
	identity agentidentity.StoredIdentity
	token    string
	saved    bool
	cleared  bool
	clearErr error
}

func (s *memoryIdentityStore) LoadIdentity(context.Context) (agentidentity.StoredIdentity, error) {
	return s.identity, nil
}
func (s *memoryIdentityStore) SaveEnrollmentIdentity(_ context.Context, value agentidentity.StoredIdentity) error {
	s.identity, s.saved = value, true
	return nil
}
func (s *memoryIdentityStore) EnrollmentToken(context.Context) (string, error) { return s.token, nil }
func (s *memoryIdentityStore) SaveCertificate(_ context.Context, value agentidentity.Certificate) error {
	s.identity.InstallationID, s.identity.CertificatePEM, s.identity.CACertificatePEM, s.identity.ExpiresAt = value.InstallationID, value.CertificatePEM, value.CACertificatePEM, value.ExpiresAt
	return nil
}
func (s *memoryIdentityStore) ClearEnrollmentToken(context.Context) error {
	if s.clearErr != nil {
		return s.clearErr
	}
	s.cleared = true
	s.token = ""
	return nil
}

func TestRunnerResumesTokenRemovalAfterCertificateWasPersisted(t *testing.T) {
	store := &memoryIdentityStore{token: "token", clearErr: errors.New("temporary Kubernetes error")}
	status := NewStatus()
	connector := &callbackConnector{}
	certificate := agentidentity.Certificate{InstallationID: "agi-abcdefghijklmnopqrst", CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), ExpiresAt: time.Now().Add(time.Hour)}
	runner := NewRunner(store, fixedEnroller{certificate: certificate}, connector, status)
	runner.ValidateCertificate = func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error { return nil }

	if err := runner.ReconcileOnce(t.Context()); err == nil || len(store.identity.CertificatePEM) == 0 || store.token == "" {
		t.Fatalf("first reconcile err=%v certificate=%q token=%q", err, store.identity.CertificatePEM, store.token)
	}
	store.clearErr = nil
	if err := runner.ReconcileOnce(t.Context()); err == nil || !store.cleared || store.token != "" || !connector.called {
		t.Fatalf("resumed reconcile err=%v cleared=%v token=%q connector=%v", err, store.cleared, store.token, connector.called)
	}
}

type fixedEnroller struct{ certificate agentidentity.Certificate }

func (e fixedEnroller) Enroll(context.Context, string, string, []byte) (agentidentity.Certificate, error) {
	return e.certificate, nil
}

type callbackConnector struct{ called, paired bool }

func (c *callbackConnector) Connect(_ context.Context, _ agentidentity.StoredIdentity, paired func()) error {
	c.called = true
	paired()
	c.paired = true
	return errors.New("stream closed")
}

func TestRunnerPersistsRetryIdentityBeforeWaitingForToken(t *testing.T) {
	store := &memoryIdentityStore{}
	status := NewStatus()
	runner := NewRunner(store, nil, nil, status)
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !store.saved || store.identity.AttemptID == "" || status.Snapshot().State != StateUnpaired || !status.Ready() {
		t.Fatalf("saved=%v identity=%+v status=%+v", store.saved, store.identity, status.Snapshot())
	}
}

func TestRunnerEnrollsPersistsAndPairs(t *testing.T) {
	store := &memoryIdentityStore{token: "token"}
	status := NewStatus()
	connector := &callbackConnector{}
	certificate := agentidentity.Certificate{InstallationID: "agi-abcdefghijklmnopqrst", CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), ExpiresAt: time.Now().Add(time.Hour)}
	runner := NewRunner(store, fixedEnroller{certificate: certificate}, connector, status)
	runner.ValidateCertificate = func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error { return nil }
	err := runner.ReconcileOnce(t.Context())
	if err == nil || !connector.called || !connector.paired || !store.cleared || status.Snapshot().State != StateConnecting {
		t.Fatalf("connector=%v cleared=%v state=%+v err=%v", connector.called, store.cleared, status.Snapshot(), err)
	}
}
