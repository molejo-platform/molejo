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
	"testing"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type recordingAgentRegistry struct {
	activatedID string
	touchedID   string
	fingerprint []byte
	activateErr error
}

func (r *recordingAgentRegistry) ActivateAgent(_ context.Context, publicID string, fingerprint []byte, _ time.Time, _ audit.Event) (bool, error) {
	r.activatedID, r.fingerprint = publicID, append([]byte(nil), fingerprint...)
	return true, r.activateErr
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
			if err := stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: &clusteragentv1alpha1.AgentHello{InstallationId: test.helloID, AgentVersion: "test"}}}); err != nil {
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
	clusteragentv1alpha1.RegisterClusterAgentServiceServer(grpcServer, NewGRPCService(registry, 30*time.Second))
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

func (r *recordingAgentRegistry) TouchAgent(_ context.Context, publicID string, fingerprint []byte, _ time.Time) error {
	r.touchedID = publicID
	if string(fingerprint) != string(r.fingerprint) {
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
	clusteragentv1alpha1.RegisterClusterAgentServiceServer(grpcServer, NewGRPCService(registry, 30*time.Second))
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
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: &clusteragentv1alpha1.AgentHello{InstallationId: installationID, AgentVersion: "test"}}}); err != nil {
		t.Fatal(err)
	}
	hello, err := stream.Recv()
	if err != nil || hello.GetHello().GetHeartbeatIntervalSeconds() != 30 {
		t.Fatalf("hello=%+v err=%v", hello, err)
	}
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Heartbeat{Heartbeat: &clusteragentv1alpha1.Heartbeat{Sequence: 7, SentAtUnix: time.Now().Unix()}}}); err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil || ack.GetHeartbeatAck().GetSequence() != 7 || registry.activatedID != installationID || registry.touchedID != installationID {
		t.Fatalf("ack=%+v activated=%q touched=%q err=%v", ack, registry.activatedID, registry.touchedID, err)
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
