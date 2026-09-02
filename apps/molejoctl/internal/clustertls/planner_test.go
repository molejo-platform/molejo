package clustertls

import (
	"path/filepath"
	"testing"
	"time"
)

func TestExistingSecretPlanIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	profile := validProfile(DriverExistingSecret, ManagementExternal)
	facts := Facts{Certificate: CertificateFacts{Exists: true, Valid: true, KeyMatches: true, DomainsCovered: true, NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)}}
	registry := NewRegistry(ExistingSecretDriver{})

	first := registry.Build(profile, facts)
	if !first.Valid() || len(first.Operations) != 1 || first.Operations[0].Kind != OperationWriteBinding || first.Binding == nil {
		t.Fatalf("first plan=%+v", first)
	}
	facts.Binding = first.Binding
	second := registry.Build(profile, facts)
	if !second.Valid() || len(second.Operations) != 0 || second.Binding == nil {
		t.Fatalf("second plan=%+v", second)
	}
}

func TestExistingSecretRejectsInvalidCertificate(t *testing.T) {
	profile := validProfile(DriverExistingSecret, ManagementExternal)
	plan := NewRegistry(ExistingSecretDriver{}).Build(profile, Facts{Certificate: CertificateFacts{Exists: true, Problem: "certificate does not cover *.apps.molejo.dev"}})
	if plan.Valid() || len(plan.Diagnostics) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestCertManagerPlansInstallIssuerAndCertificate(t *testing.T) {
	profile := validProfile(DriverCertManager, ManagementManaged)
	profile.Spec.Certificate.Config = map[string]any{
		"issuer":    map[string]any{"type": "acme", "environment": "staging", "email": "owner@example.com"},
		"challenge": map[string]any{"type": "dns01", "solver": "cloudflare", "credentialSecretRef": map[string]any{"namespace": "cert-manager", "name": "cloudflare-dns-token"}},
	}
	plan := NewRegistry(CertManagerDriver{}).Build(profile, Facts{CertManager: CertManagerFacts{CredentialExists: true}})
	if !plan.Valid() || len(plan.Operations) != 5 {
		t.Fatalf("plan=%+v", plan)
	}
	want := []OperationKind{OperationEnsureHelmRelease, OperationEnsureObject, OperationWaitForCondition, OperationEnsureObject, OperationWaitForCondition}
	for index, operation := range plan.Operations {
		if operation.Kind != want[index] {
			t.Fatalf("operation[%d]=%s, want %s", index, operation.Kind, want[index])
		}
	}
}

func TestCertManagerRequiresCredentialSecret(t *testing.T) {
	profile := validProfile(DriverCertManager, ManagementManaged)
	profile.Spec.Certificate.Config = map[string]any{
		"issuer":    map[string]any{"type": "acme", "environment": "staging", "email": "owner@example.com"},
		"challenge": map[string]any{"type": "dns01", "solver": "cloudflare", "credentialSecretRef": map[string]any{"namespace": "cert-manager", "name": "cloudflare-dns-token"}},
	}
	plan := NewRegistry(CertManagerDriver{}).Build(profile, Facts{})
	if plan.Valid() {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestCertManagerReadyCertificateWritesBindingOnce(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	profile := validProfile(DriverCertManager, ManagementManaged)
	profile.Spec.Certificate.Config = map[string]any{
		"issuer":    map[string]any{"type": "acme", "environment": "production", "email": "owner@example.com"},
		"challenge": map[string]any{"type": "dns01", "solver": "cloudflare", "credentialSecretRef": map[string]any{"namespace": "cert-manager", "name": "cloudflare-dns-token"}},
	}
	facts := Facts{
		Certificate: CertificateFacts{Exists: true, Valid: true, KeyMatches: true, DomainsCovered: true, NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)},
		CertManager: CertManagerFacts{Installed: true, CredentialExists: true, Issuer: ManagedResourceFacts{Exists: true, Owned: true, Ready: true}, Certificate: ManagedResourceFacts{Exists: true, Owned: true, Ready: true}},
	}
	registry := NewRegistry(CertManagerDriver{})
	first := registry.Build(profile, facts)
	if !first.Valid() || len(first.Operations) != 1 || first.Binding == nil || first.Binding.Status.Renewal != "Automatic" {
		t.Fatalf("first plan=%+v", first)
	}
	facts.Binding = first.Binding
	second := registry.Build(profile, facts)
	if !second.Valid() || len(second.Operations) != 0 {
		t.Fatalf("second plan=%+v", second)
	}
}

func TestProfileNormalizesAndSortsDomains(t *testing.T) {
	profile := validProfile(DriverExistingSecret, ManagementExternal)
	profile.Spec.Domains = []string{"API.EXAMPLE.COM.", "*.apps.example.com", "api.example.com"}
	normalized, diagnostics := NormalizeAndValidate(profile)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	if len(normalized.Spec.Domains) != 2 || normalized.Spec.Domains[0] != "*.apps.example.com" || normalized.Spec.Domains[1] != "api.example.com" {
		t.Fatalf("domains=%v", normalized.Spec.Domains)
	}
}

func TestPublishedTLSExamplesMatchTheProfileContract(t *testing.T) {
	for _, name := range []string{"tls-existing-secret.yaml", "tls-cert-manager-cloudflare.yaml"} {
		profile, err := LoadProfile(filepath.Join("..", "..", "..", "..", "deploy", "examples", name))
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if _, diagnostics := NormalizeAndValidate(profile); len(diagnostics) != 0 {
			t.Fatalf("%s diagnostics=%+v", name, diagnostics)
		}
	}
}

func validProfile(driver, management string) Profile {
	return Profile{
		APIVersion: APIVersion,
		Kind:       ProfileKind,
		Metadata:   Metadata{Name: "default"},
		Spec: ProfileSpec{
			Management: management,
			Domains:    []string{"*.apps.molejo.dev"},
			Certificate: CertificateSpec{
				Driver:          driver,
				TargetSecretRef: ObjectReference{Namespace: "molejo-system", Name: "apps-molejo-dev-tls"},
			},
		},
	}
}
