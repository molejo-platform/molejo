package controlplaneinstall

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"testing"
	"time"
)

func TestControlPlaneCertificateChain(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	agentCA, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := newServerCertificateAuthority(now)
	if err != nil {
		t.Fatal(err)
	}
	dnsNames := controlPlaneServerDNSNames
	identity, err := newServerIdentity(ca, dnsNames, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tls.X509KeyPair(identity.certificatePEM, identity.privateKeyPEM); err != nil {
		t.Fatalf("server key pair: %v", err)
	}
	caBlock, _ := pem.Decode(ca.certificatePEM)
	serverBlock, _ := pem.Decode(identity.certificatePEM)
	caCertificate, _ := x509.ParseCertificate(caBlock.Bytes)
	serverCertificate, _ := x509.ParseCertificate(serverBlock.Bytes)
	pool := x509.NewCertPool()
	pool.AddCert(caCertificate)
	if _, err = serverCertificate.Verify(x509.VerifyOptions{Roots: pool, DNSName: dnsNames[2], CurrentTime: now}); err != nil {
		t.Fatalf("verify server certificate: %v", err)
	}
	if !slices.Equal(serverCertificate.DNSNames, dnsNames) {
		t.Fatalf("DNS names=%v, want %v", serverCertificate.DNSNames, dnsNames)
	}
	if serverIdentitySignedBy(identity, agentCA, now) {
		t.Fatal("server identity must not be trusted by the Agent client-identity CA")
	}
	if serverIdentitySignedBy(identity, ca, now.AddDate(3, 0, 0)) {
		t.Fatal("expired server identity was accepted")
	}
}
