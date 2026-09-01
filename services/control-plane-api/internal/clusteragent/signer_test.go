package clusteragent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"
)

func TestSignerIgnoresRequestedIdentityAndIssuesBoundClientCertificate(t *testing.T) {
	caCert, caKey := testCA(t)
	signer, err := NewSigner(caCert, caKey, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requestedURI, _ := url.Parse("spiffe://attacker.invalid/admin")
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "attacker"}, DNSNames: []string{"attacker.invalid"}, URIs: []*url.URL{requestedURI},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := signer.Sign("agi-abcdefghijklmnopqrst", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(issued.CertificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.Subject.String() != "" || len(certificate.DNSNames) != 0 {
		t.Fatalf("request-controlled identity leaked into certificate: %+v", certificate)
	}
	if len(certificate.URIs) != 1 || certificate.URIs[0].String() != "spiffe://molejo.dev/agent/agi-abcdefghijklmnopqrst" {
		t.Fatalf("URIs=%v", certificate.URIs)
	}
	if len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("extended key usage=%v", certificate.ExtKeyUsage)
	}
}

func TestSignerRejectsNonP256CSR(t *testing.T) {
	caCert, caKey := testCA(t)
	signer, err := NewSigner(caCert, caKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	csrDER, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if _, err = signer.Sign("agi-abcdefghijklmnopqrst", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), time.Now()); err == nil {
		t.Fatal("P-384 CSR was accepted")
	}
}

func TestSignerAcceptsStandardSEC1ECPrivateKey(t *testing.T) {
	caCert, caKey := testCA(t)
	block, _ := pem.Decode(caKey)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sec1, err := x509.MarshalECPrivateKey(parsed.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewSigner(caCert, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1}), time.Hour); err != nil {
		t.Fatalf("SEC1 ECDSA key was rejected: %v", err)
	}
}

func testCA(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Molejo Agent CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
