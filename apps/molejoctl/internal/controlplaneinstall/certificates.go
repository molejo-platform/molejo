package controlplaneinstall

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type certificateAuthority struct {
	certificatePEM []byte
	privateKeyPEM  []byte
}

type serverIdentity struct {
	certificatePEM []byte
	privateKeyPEM  []byte
}

var controlPlaneServerDNSNames = []string{
	"control-plane-api",
	"control-plane-api.molejo-control-plane.svc",
	"control-plane-api.molejo-control-plane.svc.cluster.local",
}

func newCertificateAuthority(now time.Time) (certificateAuthority, error) {
	return newNamedCertificateAuthority("Molejo Agent Identity CA", now)
}

func newServerCertificateAuthority(now time.Time) (certificateAuthority, error) {
	return newNamedCertificateAuthority("Molejo Control Plane Server CA", now)
}

func newNamedCertificateAuthority(commonName string, now time.Time) (certificateAuthority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return certificateAuthority{}, fmt.Errorf("generate Agent CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return certificateAuthority{}, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return certificateAuthority{}, fmt.Errorf("create Agent CA certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return certificateAuthority{}, fmt.Errorf("encode Agent CA key: %w", err)
	}
	return certificateAuthority{
		certificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		privateKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}, nil
}

func serverIdentitySignedBy(identity serverIdentity, ca certificateAuthority, now time.Time) bool {
	caBlock, _ := pem.Decode(ca.certificatePEM)
	identityBlock, _ := pem.Decode(identity.certificatePEM)
	if caBlock == nil || identityBlock == nil {
		return false
	}
	caCertificate, caErr := x509.ParseCertificate(caBlock.Bytes)
	certificate, certificateErr := x509.ParseCertificate(identityBlock.Bytes)
	if caErr != nil || certificateErr != nil {
		return false
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCertificate)
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return false
	}
	for _, name := range controlPlaneServerDNSNames {
		if certificate.VerifyHostname(name) != nil {
			return false
		}
	}
	return true
}

func newServerIdentity(ca certificateAuthority, dnsNames []string, now time.Time) (serverIdentity, error) {
	caCertificate, caKey, err := parseCertificateAuthority(ca)
	if err != nil {
		return serverIdentity{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return serverIdentity{}, fmt.Errorf("generate control plane server key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return serverIdentity{}, err
	}
	if len(dnsNames) == 0 {
		return serverIdentity{}, errors.New("control plane server DNS names are required")
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: dnsNames[len(dnsNames)-1]},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(2, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              append([]string(nil), dnsNames...),
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, &key.PublicKey, caKey)
	if err != nil {
		return serverIdentity{}, fmt.Errorf("create control plane server certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return serverIdentity{}, fmt.Errorf("encode control plane server key: %w", err)
	}
	return serverIdentity{
		certificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		privateKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}, nil
}

func parseCertificateAuthority(ca certificateAuthority) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certificateBlock, _ := pem.Decode(ca.certificatePEM)
	keyBlock, _ := pem.Decode(ca.privateKeyPEM)
	if certificateBlock == nil || keyBlock == nil {
		return nil, nil, errors.New("Agent CA material is invalid")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil || !certificate.IsCA {
		return nil, nil, errors.New("Agent CA certificate is invalid")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if err != nil || !ok || !publicKey.Equal(&key.PublicKey) {
		return nil, nil, errors.New("Agent CA key is invalid")
	}
	return certificate, key, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	return serial, nil
}
