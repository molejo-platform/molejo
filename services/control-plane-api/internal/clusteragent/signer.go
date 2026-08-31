package clusteragent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"time"
)

var ErrInvalidCSR = errors.New("agent certificate request is invalid")

type Signer struct {
	ca       *x509.Certificate
	key      *ecdsa.PrivateKey
	caPEM    []byte
	validity time.Duration
}

type IssuedCertificate struct {
	CertificatePEM   []byte
	CACertificatePEM []byte
	Serial           string
	Fingerprint      []byte
	NotAfter         time.Time
}

func NewSigner(certificatePEM, privateKeyPEM []byte, validity time.Duration) (*Signer, error) {
	certificateBlock, _ := pem.Decode(certificatePEM)
	privateKeyBlock, _ := pem.Decode(privateKeyPEM)
	if certificateBlock == nil || privateKeyBlock == nil || validity <= 0 {
		return nil, fmt.Errorf("agent CA material is invalid")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil || !certificate.IsCA {
		return nil, fmt.Errorf("agent CA certificate is invalid")
	}
	key, err := parseECDSAPrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse agent CA key: %w", err)
	}
	caPublicKey, caUsesECDSA := certificate.PublicKey.(*ecdsa.PublicKey)
	if !caUsesECDSA || key.Curve != elliptic.P256() || caPublicKey.Curve != elliptic.P256() || !caPublicKey.Equal(&key.PublicKey) {
		return nil, fmt.Errorf("agent CA must use a matching ECDSA P-256 key")
	}
	return &Signer{ca: certificate, key: key, caPEM: append([]byte(nil), certificatePEM...), validity: validity}, nil
}

func parseECDSAPrivateKey(der []byte) (*ecdsa.PrivateKey, error) {
	parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(der)
	if pkcs8Err == nil {
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not ECDSA")
		}
		return key, nil
	}
	key, sec1Err := x509.ParseECPrivateKey(der)
	if sec1Err != nil {
		return nil, errors.Join(pkcs8Err, sec1Err)
	}
	return key, nil
}

func (s *Signer) Sign(installationID string, csrPEM []byte, now time.Time) (IssuedCertificate, error) {
	block, rest := pem.Decode(csrPEM)
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE REQUEST" {
		return IssuedCertificate{}, ErrInvalidCSR
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return IssuedCertificate{}, ErrInvalidCSR
	}
	publicKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() {
		return IssuedCertificate{}, ErrInvalidCSR
	}
	uri, err := url.Parse("spiffe://molejo.dev/agent/" + installationID)
	if err != nil {
		return IssuedCertificate{}, fmt.Errorf("build agent identity: %w", err)
	}
	serialBytes := make([]byte, 16)
	if _, err = rand.Read(serialBytes); err != nil {
		return IssuedCertificate{}, fmt.Errorf("generate certificate serial: %w", err)
	}
	serial := new(big.Int).SetBytes(serialBytes)
	template := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    now.Add(-time.Minute), NotAfter: now.Add(s.validity),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:        []*url.URL{uri}, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, s.ca, publicKey, s.key)
	if err != nil {
		return IssuedCertificate{}, fmt.Errorf("sign agent certificate: %w", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return IssuedCertificate{}, fmt.Errorf("parse issued certificate: %w", err)
	}
	digest := sha256.Sum256(der)
	return IssuedCertificate{
		CertificatePEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		CACertificatePEM: append([]byte(nil), s.caPEM...),
		Serial:           hex.EncodeToString(serialBytes), Fingerprint: digest[:], NotAfter: parsed.NotAfter,
	}, nil
}
