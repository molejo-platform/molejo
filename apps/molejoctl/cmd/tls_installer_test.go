package cmd

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

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clustertls"
)

type fakeTLSEnvironment struct {
	facts      clustertls.Facts
	operations []clustertls.Operation
}

func (f *fakeTLSEnvironment) Discover(context.Context, clustertls.Profile) (clustertls.Facts, error) {
	return f.facts, nil
}

func (f *fakeTLSEnvironment) Execute(_ context.Context, operation clustertls.Operation) error {
	f.operations = append(f.operations, operation)
	if operation.Binding != nil {
		f.facts.Binding = operation.Binding
	}
	return nil
}

func TestTLSConfigureUseCaseConvergesAndRequiresApproval(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	environment := &fakeTLSEnvironment{facts: clustertls.Facts{Certificate: clustertls.CertificateFacts{Exists: true, Valid: true, KeyMatches: true, DomainsCovered: true, NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)}}}
	configurator := kubernetesTLSConfigurator{
		registry:       clustertls.NewRegistry(clustertls.ExistingSecretDriver{}),
		newEnvironment: func(string) (tlsEnvironment, error) { return environment, nil },
	}
	profilePath := filepath.Join(t.TempDir(), "tls.yaml")
	profile := "apiVersion: platform.molejo.dev/v1alpha1\nkind: ClusterTLSProfile\nmetadata:\n  name: default\nspec:\n  management: External\n  domains: [\"*.apps.molejo.dev\"]\n  certificate:\n    driver: existing-secret\n    targetSecretRef: {namespace: molejo-system, name: apps-molejo-dev-tls}\n"
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	output := &bytes.Buffer{}
	if _, err := configurator.Configure(t.Context(), tlsConfigureOptions{contextName: "molejo-k3s", profilePath: profilePath, output: output}); err == nil {
		t.Fatal("expected approval error")
	}
	report, err := configurator.Configure(t.Context(), tlsConfigureOptions{contextName: "molejo-k3s", profilePath: profilePath, output: output, yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.alreadyConfigured || len(environment.operations) != 1 || report.binding.Metadata.Name != "default" {
		t.Fatalf("report=%+v operations=%d", report, len(environment.operations))
	}
}

func TestInspectTLSSecretValidatesKeyDomainsAndLifetime(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	certificate, key := testTLSMaterial(t, []string{"*.apps.molejo.dev"}, now)
	secret := &corev1.Secret{Type: corev1.SecretTypeTLS, Data: map[string][]byte{corev1.TLSCertKey: certificate, corev1.TLSPrivateKeyKey: key}}

	facts := inspectTLSSecret(secret, []string{"*.apps.molejo.dev"}, now)
	if !facts.Valid || !facts.KeyMatches || !facts.DomainsCovered {
		t.Fatalf("facts=%+v", facts)
	}
	invalid := inspectTLSSecret(secret, []string{"api.example.com"}, now)
	if invalid.Valid || invalid.Problem == "" {
		t.Fatalf("invalid facts=%+v", invalid)
	}
}

func testTLSMaterial(t *testing.T, domains []string, now time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domains[0]}, DNSNames: domains, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
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
