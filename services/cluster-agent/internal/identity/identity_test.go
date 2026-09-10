package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestNewEnrollmentIdentityUsesP256AndPersistsRetryMaterial(t *testing.T) {
	value, err := NewEnrollmentIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if value.AttemptID == "" || len(value.PrivateKeyPEM) == 0 || len(value.CSRPEM) == 0 {
		t.Fatalf("identity is incomplete: %+v", value)
	}
	keyBlock, _ := pem.Decode(value.PrivateKeyPEM)
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok || ecdsaKey.Curve != elliptic.P256() {
		t.Fatalf("private key=%T curve=%v", key, ecdsaKey.Curve)
	}
	csrBlock, _ := pem.Decode(value.CSRPEM)
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		t.Fatalf("CSR is invalid: %v", err)
	}
	if csr.Subject.String() != "" || len(csr.DNSNames) != 0 || len(csr.URIs) != 0 {
		t.Fatalf("CSR requested identity fields: %+v", csr)
	}
}
