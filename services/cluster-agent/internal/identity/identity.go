package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base32"
	"encoding/pem"
	"fmt"
	"strings"
)

type EnrollmentIdentity struct {
	AttemptID     string
	PrivateKeyPEM []byte
	CSRPEM        []byte
}

func NewEnrollmentIdentity() (EnrollmentIdentity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return EnrollmentIdentity{}, fmt.Errorf("generate private key: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return EnrollmentIdentity{}, fmt.Errorf("marshal private key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return EnrollmentIdentity{}, fmt.Errorf("create certificate request: %w", err)
	}
	attemptID, err := randomID()
	if err != nil {
		return EnrollmentIdentity{}, err
	}
	return EnrollmentIdentity{
		AttemptID:     attemptID,
		PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		CSRPEM:        pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}),
	}, nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate attempt ID: %w", err)
	}
	return "ena-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(value)), nil
}
