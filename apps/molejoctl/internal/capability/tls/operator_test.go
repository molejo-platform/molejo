package tls

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

type fakeTLSEnvironment struct {
	initial    Facts
	ready      Facts
	operations []Operation
}

func (f *fakeTLSEnvironment) Discover(context.Context, Setup) (Facts, error) {
	if len(f.operations) > 0 {
		return f.ready, nil
	}
	return f.initial, nil
}

func (f *fakeTLSEnvironment) Execute(_ context.Context, operation Operation) error {
	f.operations = append(f.operations, operation)
	return nil
}

func TestTLSPrepareConvergesAndRequiresApproval(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	readyFacts := Facts{
		Certificate: CertificateFacts{Exists: true, Valid: true, KeyMatches: true, DNSNamesCovered: true, NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)},
		CertManager: CertManagerFacts{NamespaceExists: true, Installed: true, VersionMatches: true, Credential: CredentialFacts{Exists: true, Owned: true, Usable: true, Matches: true}, Issuer: ManagedResourceFacts{Exists: true, Owned: true, Ready: true, Matches: true}, Certificate: ManagedResourceFacts{Exists: true, Owned: true, Ready: true, Matches: true}},
	}
	environment := &fakeTLSEnvironment{initial: Facts{CertManager: CertManagerFacts{NamespaceExists: true, Installed: true, VersionMatches: true, Credential: CredentialFacts{Exists: true, Owned: true, Usable: true, Matches: true}}}, ready: readyFacts}
	operator := Operator{newEnvironment: func(string, []byte) (tlsEnvironment, error) { return environment, nil }}
	setupPath := writeTLSSetup(t, true)
	t.Setenv("CLOUDFLARE_TOKEN", "test-token")

	if _, err := operator.Prepare(t.Context(), Options{ContextName: "molejo-k3s", SetupPath: setupPath, CredentialEnv: "CLOUDFLARE_TOKEN", Output: &bytes.Buffer{}}); err == nil {
		t.Fatal("expected approval error")
	}
	report, err := operator.Prepare(t.Context(), Options{ContextName: "molejo-k3s", SetupPath: setupPath, CredentialEnv: "CLOUDFLARE_TOKEN", Output: &bytes.Buffer{}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Changed || !report.Certificate.Valid || len(environment.operations) != 4 {
		t.Fatalf("report=%+v operations=%d", report, len(environment.operations))
	}
}

func TestTLSVerifyOnlyRequiresExistingMaterial(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	environment := &fakeTLSEnvironment{initial: Facts{Certificate: CertificateFacts{Exists: true, Valid: true, KeyMatches: true, DNSNamesCovered: true, NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)}}}
	operator := Operator{newEnvironment: func(string, []byte) (tlsEnvironment, error) { return environment, nil }}
	report, err := operator.Verify(t.Context(), Options{ContextName: "molejo-k3s", SetupPath: writeTLSSetup(t, false)})
	if err != nil || !report.Certificate.Valid || len(environment.operations) != 0 {
		t.Fatalf("report=%+v operations=%d err=%v", report, len(environment.operations), err)
	}
}

func TestCredentialEnvironmentMustContainAValue(t *testing.T) {
	t.Setenv("EMPTY_CREDENTIAL", "")
	if _, err := credentialFromEnvironment("EMPTY_CREDENTIAL"); err == nil {
		t.Fatal("expected empty credential error")
	}
}

func TestInspectTLSSecretValidatesKeyDNSNamesAndLifetime(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	certificate, key := testTLSMaterial(t, []string{"*.apps.molejo.dev"}, now)
	secret := &corev1.Secret{Type: corev1.SecretTypeTLS, Data: map[string][]byte{corev1.TLSCertKey: certificate, corev1.TLSPrivateKeyKey: key}}
	facts := inspectTLSSecret(secret, []string{"*.apps.molejo.dev"}, now)
	if !facts.Valid || !facts.KeyMatches || !facts.DNSNamesCovered {
		t.Fatalf("facts=%+v", facts)
	}
	invalid := inspectTLSSecret(secret, []string{"api.example.com"}, now)
	if invalid.Valid || invalid.Problem == "" {
		t.Fatalf("invalid facts=%+v", invalid)
	}
}

func writeTLSSetup(t *testing.T, recipe bool) string {
	t.Helper()
	contents := "apiVersion: config.molejo.dev/v1alpha1\nkind: TLSSetup\nmetadata:\n  name: molejo-dev\nspec:\n  dnsNames: [\"*.molejo.dev\"]\n  targetSecretRef: {namespace: molejo-system, name: molejo-dev-tls}\n"
	if recipe {
		contents += "  recipe:\n    id: cert-manager-cloudflare\n    config:\n      issuer: {type: acme, environment: staging, email: owner@example.com}\n      challenge: {type: dns01, solver: cloudflare, credentialSecretRef: {namespace: cert-manager, name: cloudflare-dns-token}}\n"
	}
	path := filepath.Join(t.TempDir(), "tls.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testTLSMaterial(t *testing.T, dnsNames []string, now time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dnsNames[0]}, DNSNames: dnsNames, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
