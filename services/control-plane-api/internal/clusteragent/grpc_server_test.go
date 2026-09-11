package clusteragent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/packages/workspacecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

type recordingAgentRegistry struct {
	activatedID        string
	touchedID          string
	sessionID          string
	sequence           uint64
	fingerprint        []byte
	activateErr        error
	provisioningMode   workspacecontract.ProvisioningMode
	observed           []store.RuntimeObservation
	complete           bool
	capabilities       []capabilitycontract.Observation
	capabilityComplete bool
	bindingTargets     []kubernetesbinding.Target
	bindings           []kubernetesbinding.Observation
	bindingComplete    bool
}

func (r *recordingAgentRegistry) ActivateAgent(_ context.Context, publicID string, fingerprint []byte, _, _, _ string, _ []string, mode workspacecontract.ProvisioningMode, _, sessionID string, _ time.Time, _ audit.Event) (bool, error) {
	r.activatedID, r.fingerprint, r.sessionID, r.provisioningMode = publicID, append([]byte(nil), fingerprint...), sessionID, mode
	return true, r.activateErr
}

func (r *recordingAgentRegistry) RenewAgent(_ context.Context, publicID string, _ []byte, _ string, _ []byte, _ time.Time, issue func(string) (store.AgentCertificate, error), _ audit.Event) (store.AgentCertificate, error) {
	if r.activateErr != nil {
		return store.AgentCertificate{}, r.activateErr
	}
	certificate, err := issue(publicID)
	certificate.InstallationID = publicID
	return certificate, err
}

func (r *recordingAgentRegistry) ReconcileAgentObservations(_ context.Context, _, _ string, _ uint64, observations []store.RuntimeObservation, complete bool) error {
	r.observed, r.complete = observations, complete
	return nil
}

func (r *recordingAgentRegistry) ReconcileCapabilityObservations(_ context.Context, _, _ string, _ uint64, observations []capabilitycontract.Observation, complete bool, _ time.Time) error {
	r.capabilities, r.capabilityComplete = observations, complete
	return nil
}

func (r *recordingAgentRegistry) BindingObservationTargets(context.Context, string) ([]kubernetesbinding.Target, error) {
	return append([]kubernetesbinding.Target{}, r.bindingTargets...), nil
}

func (r *recordingAgentRegistry) ReconcileBindingObservations(_ context.Context, _, _ string, _ uint64, observations []kubernetesbinding.Observation, complete bool, _ time.Time) error {
	r.bindings, r.bindingComplete = append([]kubernetesbinding.Observation{}, observations...), complete
	return nil
}

func TestGRPCServiceRejectsMismatchedOrInactiveInstallation(t *testing.T) {
	for _, test := range []struct {
		name          string
		helloID       string
		activationErr error
	}{
		{name: "declared installation differs from certificate", helloID: "agi-bbbbbbbbbbbbbbbbbbbb"},
		{name: "certificate is unknown", helloID: "agi-abcdefghijklmnopqrst", activationErr: ErrPeerIdentityMismatch},
		{name: "installation is revoked", helloID: "agi-abcdefghijklmnopqrst", activationErr: ErrPeerIdentityMismatch},
		{name: "certificate is expired in registry", helloID: "agi-abcdefghijklmnopqrst", activationErr: ErrPeerIdentityMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := &recordingAgentRegistry{activateErr: test.activationErr}
			stream := authenticatedTestStream(t, registry, "agi-abcdefghijklmnopqrst")
			if err := stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: testAgentHello(test.helloID)}}); err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Recv(); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("Recv error=%v, want PermissionDenied", err)
			}
		})
	}
}

func authenticatedTestStream(t *testing.T, registry AgentRegistry, installationID string) clusteragentv1alpha1.ClusterAgentService_ConnectClient {
	t.Helper()
	caCertificate, caKey := testCA(t)
	clientKey, csr := testCSRAndKey(t)
	signer, err := NewSigner(caCertificate, caKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := signer.Sign(installationID, csr, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, err := tls.X509KeyPair(issued.CertificatePEM, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	serverCertificate := testServerCertificate(t, caCertificate, caKey)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caCertificate)
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots})))
	clusteragentv1alpha1.RegisterClusterAgentServiceServer(grpcServer, NewGRPCService(registry, nil, 30*time.Second))
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	connection, err := grpc.NewClient("passthrough:///control-plane.test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: "control-plane.test", RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	stream, err := clusteragentv1alpha1.NewClusterAgentServiceClient(connection).Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func (r *recordingAgentRegistry) TouchAgent(_ context.Context, publicID string, fingerprint []byte, sessionID string, sequence uint64, _ time.Time) error {
	r.touchedID, r.sequence = publicID, sequence
	if string(fingerprint) != string(r.fingerprint) || sessionID != r.sessionID {
		return ErrPeerIdentityMismatch
	}
	return nil
}

func TestGRPCServiceAuthenticatesHelloAndAcknowledgesHeartbeat(t *testing.T) {
	const installationID = "agi-abcdefghijklmnopqrst"
	caCertificate, caKey := testCA(t)
	clientKey, csr := testCSRAndKey(t)
	signer, err := NewSigner(caCertificate, caKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := signer.Sign(installationID, csr, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, err := tls.X509KeyPair(issued.CertificatePEM, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	serverCertificate := testServerCertificate(t, caCertificate, caKey)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caCertificate)
	registry := &recordingAgentRegistry{}
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots})))
	clusteragentv1alpha1.RegisterClusterAgentServiceServer(grpcServer, NewGRPCService(registry, nil, 30*time.Second))
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	connection, err := grpc.NewClient("passthrough:///control-plane.test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: "control-plane.test", RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	stream, err := clusteragentv1alpha1.NewClusterAgentServiceClient(connection).Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: testAgentHello(installationID)}}); err != nil {
		t.Fatal(err)
	}
	hello, err := stream.Recv()
	if err != nil || hello.GetHello().GetHeartbeatIntervalSeconds() != 30 || hello.GetHello().GetSessionId() == "" {
		t.Fatalf("hello=%+v err=%v", hello, err)
	}
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Heartbeat{Heartbeat: &clusteragentv1alpha1.Heartbeat{Sequence: 7, SentAtUnix: time.Now().Unix(), SessionId: hello.GetHello().GetSessionId(), ObservationSnapshotComplete: true, Observations: []*clusteragentv1alpha1.RuntimeObservation{{Kind: "AppDeployment", Namespace: "workspace-one", Name: "ap-test", State: "Ready"}}}}}); err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil || ack.GetHeartbeatAck().GetSequence() != 7 || registry.activatedID != installationID || registry.touchedID != installationID || registry.sequence != 7 || !registry.complete || len(registry.observed) != 1 {
		t.Fatalf("ack=%+v activated=%q touched=%q err=%v", ack, registry.activatedID, registry.touchedID, err)
	}
}

func TestGRPCServiceAcceptsNegotiatedCapabilitySnapshot(t *testing.T) {
	const installationID = "agi-abcdefghijklmnopqrst"
	registry := &recordingAgentRegistry{}
	stream := authenticatedTestStream(t, registry, installationID)
	hello := testAgentHello(installationID)
	hello.Capabilities = append(hello.Capabilities, "capability-observation.v1alpha1")
	if err := stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: hello}}); err != nil {
		t.Fatal(err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := &clusteragentv1alpha1.Heartbeat{Sequence: 1, SentAtUnix: time.Now().Unix(), SessionId: response.GetHello().GetSessionId(), CapabilitySnapshotComplete: true, CapabilityObservations: []*clusteragentv1alpha1.CapabilityObservation{{CapabilityId: string(capabilitycontract.StorageRWO), ContractVersion: capabilitycontract.ContractVersion, Support: string(capabilitycontract.SupportSupported), Health: string(capabilitycontract.HealthHealthy), ProviderKind: "kubernetes", SampledAtUnix: time.Now().Unix()}}}
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Heartbeat{Heartbeat: heartbeat}}); err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if !registry.capabilityComplete || len(registry.capabilities) != 1 || registry.capabilities[0].ID != capabilitycontract.StorageRWO {
		t.Fatalf("capability snapshot=%+v complete=%v", registry.capabilities, registry.capabilityComplete)
	}
}

func TestGRPCServiceExchangesOnlyNegotiatedBindingTargetsAndObservations(t *testing.T) {
	const installationID = "agi-abcdefghijklmnopqrst"
	registry := &recordingAgentRegistry{bindingTargets: []kubernetesbinding.Target{{ID: "storage:persistent-standard", Kind: kubernetesbinding.KindStorage, Version: 3, Storage: &kubernetesbinding.StorageTarget{StorageClassName: "local-path"}}}}
	stream := authenticatedTestStream(t, registry, installationID)
	hello := testAgentHello(installationID)
	hello.Capabilities = append(hello.Capabilities, "binding-observation.v1alpha1")
	if err := stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: hello}}); err != nil {
		t.Fatal(err)
	}
	response, err := stream.Recv()
	if err != nil || !hasCapability(response.GetHello().GetCapabilities(), "binding-observation.v1alpha1") {
		t.Fatalf("hello=%+v err=%v", response, err)
	}
	first := &clusteragentv1alpha1.Heartbeat{Sequence: 1, SentAtUnix: time.Now().Unix(), SessionId: response.GetHello().GetSessionId()}
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Heartbeat{Heartbeat: first}}); err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil || len(ack.GetHeartbeatAck().GetBindingTargets()) != 1 || ack.GetHeartbeatAck().GetBindingTargets()[0].GetStorage().GetStorageClassName() != "local-path" {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
	second := &clusteragentv1alpha1.Heartbeat{
		Sequence: 2, SentAtUnix: time.Now().Unix(), SessionId: response.GetHello().GetSessionId(), BindingSnapshotComplete: true,
		BindingObservations: []*clusteragentv1alpha1.BindingObservation{{Id: "storage:persistent-standard", Kind: string(kubernetesbinding.KindStorage), Version: 3, Health: string(kubernetesbinding.HealthHealthy), SampledAtUnix: time.Now().Unix(), Observation: &clusteragentv1alpha1.BindingObservation_Storage{Storage: &clusteragentv1alpha1.StorageBindingObservation{StorageClassName: "local-path", Provisioner: "rancher.io/local-path", AccessModes: []string{"ReadWriteOnce"}}}}},
	}
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Heartbeat{Heartbeat: second}}); err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if !registry.bindingComplete || len(registry.bindings) != 1 || registry.bindings[0].Version != 3 {
		t.Fatalf("bindings=%+v complete=%t", registry.bindings, registry.bindingComplete)
	}
}

func TestGRPCServiceRenewsAnAuthenticatedAgentCertificate(t *testing.T) {
	const installationID = "cls-abcdefghijklmnopqrst"
	caCertificate, caKey := testCA(t)
	clientKey, clientCSR := testCSRAndKey(t)
	signer, err := NewSigner(caCertificate, caKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := signer.Sign(installationID, clientCSR, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, err := tls.X509KeyPair(issued.CertificatePEM, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	serverCertificate := testServerCertificate(t, caCertificate, caKey)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caCertificate)
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots})))
	service := NewGRPCService(&recordingAgentRegistry{}, nil, time.Second)
	service.ConfigureCertificateRenewal(signer, caCertificate, strings.Repeat("a", 64))
	clusteragentv1alpha1.RegisterClusterAgentServiceServer(grpcServer, service)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	connection, err := grpc.NewClient("passthrough:///control-plane.test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: "control-plane.test", RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	_, renewalCSR := testCSRAndKey(t)
	response, err := clusteragentv1alpha1.NewClusterAgentServiceClient(connection).RenewCertificate(t.Context(), &clusteragentv1alpha1.RenewCertificateRequest{InstallationId: installationID, AttemptId: "renewal-attempt", CsrPem: renewalCSR})
	if err != nil || response.GetInstallationId() != installationID || len(response.GetCertificatePem()) == 0 || len(response.GetCaCertificatePem()) == 0 || len(response.GetServerCaCertificatePem()) == 0 {
		t.Fatalf("renewal response=%+v err=%v", response, err)
	}
}

func testAgentHello(installationID string) *clusteragentv1alpha1.AgentHello {
	return &clusteragentv1alpha1.AgentHello{
		InstallationId:            installationID,
		AgentVersion:              "test",
		ClusterUid:                "cluster-test-uid",
		KubernetesVersion:         "v1.36.3",
		Capabilities:              []string{"runtime.v1alpha4"},
		WorkspaceProvisioningMode: string(workspacecontract.ProvisioningNamespaced),
	}
}

func testCSRAndKey(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})
}

func testServerCertificate(t *testing.T, caCertificatePEM, caKeyPEM []byte) tls.Certificate {
	t.Helper()
	caBlock, _ := pem.Decode(caCertificatePEM)
	caCertificate, _ := x509.ParseCertificate(caBlock.Bytes)
	keyBlock, _ := pem.Decode(caKeyPEM)
	parsedKey, _ := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	caKey := parsedKey.(*ecdsa.PrivateKey)
	serverKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{}, DNSNames: []string{"control-plane.test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	serverKeyDER, _ := x509.MarshalPKCS8PrivateKey(serverKey)
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER}))
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}
