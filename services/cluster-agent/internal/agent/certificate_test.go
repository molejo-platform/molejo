package agent

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

	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

func TestValidateCertificateBindsPersistedKeyAndInstallation(t *testing.T) {
	const installationID = "agi-abcdefghijklmnopqrst"
	stored, certificate, now := agentTestCertificate(t, installationID)
	if err := ValidateCertificate(stored, certificate, now); err != nil {
		t.Fatal(err)
	}
	certificate.InstallationID = "agi-bbbbbbbbbbbbbbbbbbbb"
	if err := ValidateCertificate(stored, certificate, now); err == nil {
		t.Fatal("certificate was accepted for another installation")
	}
}

func agentTestCertificate(t *testing.T, installationID string) (agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Agent CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	uri, _ := url.Parse("spiffe://molejo.dev/agent/" + installationID)
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{uri}}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &key.PublicKey, caKey)
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certificate := agentidentity.Certificate{InstallationID: installationID, CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), CACertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), ExpiresAt: leafTemplate.NotAfter}
	return agentidentity.StoredIdentity{PrivateKeyPEM: privateKeyPEM}, certificate, now
}
