package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

func ValidateCertificate(stored agentidentity.StoredIdentity, issued agentidentity.Certificate, now time.Time) error {
	keyBlock, keyRest := pem.Decode(stored.PrivateKeyPEM)
	certificateBlock, certificateRest := pem.Decode(issued.CertificatePEM)
	caBlock, caRest := pem.Decode(issued.CACertificatePEM)
	if keyBlock == nil || certificateBlock == nil || caBlock == nil || len(keyRest) != 0 || len(certificateRest) != 0 || len(caRest) != 0 {
		return errors.New("Agent identity PEM is invalid")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return errors.New("Agent private key is invalid")
	}
	privateKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok || privateKey.Curve != elliptic.P256() {
		return errors.New("Agent private key must use ECDSA P-256")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return errors.New("Agent certificate is invalid")
	}
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || !publicKey.Equal(&privateKey.PublicKey) {
		return errors.New("Agent certificate does not match the private key")
	}
	caCertificate, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil || !caCertificate.IsCA {
		return errors.New("Agent CA certificate is invalid")
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCertificate)
	if _, err = certificate.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return fmt.Errorf("verify Agent certificate: %w", err)
	}
	expectedURI := "spiffe://molejo.dev/agent/" + issued.InstallationID
	if len(certificate.URIs) != 1 || certificate.URIs[0].String() != expectedURI {
		return errors.New("Agent certificate installation identity is invalid")
	}
	if issued.ExpiresAt.IsZero() || !certificate.NotAfter.Equal(issued.ExpiresAt) {
		return errors.New("Agent certificate expiry is inconsistent")
	}
	return nil
}
