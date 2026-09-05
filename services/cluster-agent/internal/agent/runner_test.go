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
	s.identity.InstallationID, s.identity.PrivateKeyPEM, s.identity.CertificatePEM = value.InstallationID, value.PrivateKeyPEM, value.CertificatePEM
	s.identity.CACertificatePEM, s.identity.ServerCAPEM, s.identity.TrustBundleID, s.identity.ExpiresAt = value.CACertificatePEM, value.ServerCAPEM, value.TrustBundleID, value.ExpiresAt
	s.identity.RenewalAttemptID, s.identity.RenewalKeyPEM, s.identity.RenewalCSRPEM = "", nil, nil
	return nil
}

type fixedRenewer struct {
	certificate agentidentity.Certificate
	called      bool
}

func (r *fixedRenewer) Renew(_ context.Context, _ agentidentity.StoredIdentity, request RenewalRequest) (agentidentity.Certificate, error) {
	r.called = request.AttemptID != "" && len(request.CSRPEM) > 0
	return r.certificate, nil
}

func TestRunnerRotatesCertificateBeforeConnecting(t *testing.T) {
	now := time.Now().UTC()
	store := &memoryIdentityStore{identity: agentidentity.StoredIdentity{
		InstallationID: "cls-abcdefghijklmnopqrst", PrivateKeyPEM: []byte("old-key"), CertificatePEM: []byte("old-certificate"),
		CACertificatePEM: []byte("identity-ca"), ServerCAPEM: []byte("old-server-ca"), ExpiresAt: now.Add(time.Hour),
	}}
	renewer := &fixedRenewer{certificate: agentidentity.Certificate{
		InstallationID: "cls-abcdefghijklmnopqrst", CertificatePEM: []byte("new-certificate"),
		CACertificatePEM: []byte("identity-ca"), ServerCAPEM: []byte("new-server-ca"), ExpiresAt: now.Add(7 * 24 * time.Hour),
	}}
	connector := &callbackConnector{}
	runner := NewRunner(store, nil, connector, NewStatus())
	runner.now = func() time.Time { return now }
	runner.validateCertificate = func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error { return nil }
	runner.ConfigureRenewal(renewer, 24*time.Hour)

	if err := runner.ReconcileOnce(t.Context()); err == nil {
		t.Fatal("expected the test connector to close the stream")
	}
	if !renewer.called || !connector.called || string(store.identity.CertificatePEM) != "new-certificate" || string(store.identity.PrivateKeyPEM) == "old-key" || string(store.identity.ServerCAPEM) != "new-server-ca" || store.identity.RenewalAttemptID != "" {
		t.Fatalf("rotation was not persisted atomically: renewer=%v connector=%v identity=%+v", renewer.called, connector.called, store.identity)
	}
}

func (s *memoryIdentityStore) SaveRenewalIdentity(_ context.Context, value agentidentity.StoredIdentity) error {
	s.identity = value
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
	runner.validateCertificate = func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error { return nil }

	if err := runner.ReconcileOnce(t.Context()); err == nil || len(store.identity.CertificatePEM) == 0 || store.token == "" {
		t.Fatalf("first reconcile err=%v certificate=%q token=%q", err, store.identity.CertificatePEM, store.token)
	}
	store.clearErr = nil
	if err := runner.ReconcileOnce(t.Context()); err == nil || !store.cleared || store.token != "" || !connector.called {
		t.Fatalf("resumed reconcile err=%v cleared=%v token=%q connector=%v", err, store.cleared, store.token, connector.called)
	}
}

type fixedEnroller struct{ certificate agentidentity.Certificate }

func (e fixedEnroller) Enroll(context.Context, EnrollmentRequest) (agentidentity.Certificate, error) {
	return e.certificate, nil
}

type callbackConnector struct{ called, paired bool }

func (c *callbackConnector) Connect(_ context.Context, _ agentidentity.StoredIdentity, paired func()) error {
	c.called = true
	paired()
	c.paired = true
	return errors.New("stream closed")
}

type trustRotationConnector struct {
	calls      int
	observedID string
}

func (c *trustRotationConnector) Connect(_ context.Context, identity agentidentity.StoredIdentity, paired func()) error {
	c.calls++
	c.observedID = identity.TrustBundleID
	if c.calls == 1 {
		return agentidentity.ErrTrustBundleUpdateRequired
	}
	paired()
	return errors.New("stream closed")
}

func TestRunnerRenewsImmediatelyWhenTrustBundleChanges(t *testing.T) {
	now := time.Now().UTC()
	store := &memoryIdentityStore{identity: agentidentity.StoredIdentity{
		InstallationID: "cls-abcdefghijklmnopqrst", PrivateKeyPEM: []byte("old-key"), CertificatePEM: []byte("old-certificate"),
		CACertificatePEM: []byte("old-ca"), ServerCAPEM: []byte("old-server-ca"), TrustBundleID: "old", ExpiresAt: now.Add(6 * 24 * time.Hour),
	}}
	renewer := &fixedRenewer{certificate: agentidentity.Certificate{
		InstallationID: "cls-abcdefghijklmnopqrst", CertificatePEM: []byte("new-certificate"), CACertificatePEM: []byte("new-and-old-ca"),
		ServerCAPEM: []byte("new-and-old-server-ca"), TrustBundleID: "new", ExpiresAt: now.Add(7 * 24 * time.Hour),
	}}
	connector := &trustRotationConnector{}
	runner := NewRunner(store, nil, connector, NewStatus())
	runner.now = func() time.Time { return now }
	runner.validateCertificate = func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error { return nil }
	runner.ConfigureRenewal(renewer, 24*time.Hour)

	if err := runner.ReconcileOnce(t.Context()); err == nil {
		t.Fatal("expected the replacement stream to close")
	}
	if !renewer.called || connector.calls != 2 || connector.observedID != "new" || store.identity.TrustBundleID != "new" {
		t.Fatalf("renewer=%v connector=%+v identity=%+v", renewer.called, connector, store.identity)
	}
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
	runner.validateCertificate = func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error { return nil }
	err := runner.ReconcileOnce(t.Context())
	if err == nil || !connector.called || !connector.paired || !store.cleared || status.Snapshot().State != StateConnecting {
		t.Fatalf("connector=%v cleared=%v state=%+v err=%v", connector.called, store.cleared, status.Snapshot(), err)
	}
}
