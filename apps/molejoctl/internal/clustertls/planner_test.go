package clustertls

import (
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPreparePlanOrdersCredentialCertManagerAndCertificate(t *testing.T) {
	plan := BuildPreparePlan(validSetup(true), Facts{}, true)
	if !plan.Valid() || len(plan.Operations) != 7 {
		t.Fatalf("plan=%+v", plan)
	}
	want := []OperationKind{OperationEnsureNamespace, OperationEnsureCredentialSecret, OperationEnsureHelmRelease, OperationEnsureObject, OperationWaitForCondition, OperationEnsureObject, OperationWaitForCondition}
	for index, operation := range plan.Operations {
		if operation.Kind != want[index] {
			t.Fatalf("operation[%d]=%s, want %s", index, operation.Kind, want[index])
		}
	}
}

func TestPreparePlanRequiresCredentialSourceWhenSecretIsMissing(t *testing.T) {
	plan := BuildPreparePlan(validSetup(true), Facts{}, false)
	if plan.Valid() || len(plan.Diagnostics) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestPreparePlanIsReadyWhenRecipeOutcomeExists(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	facts := Facts{
		Certificate: CertificateFacts{Exists: true, Valid: true, KeyMatches: true, DNSNamesCovered: true, NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)},
		CertManager: CertManagerFacts{NamespaceExists: true, Installed: true, VersionMatches: true, Credential: CredentialFacts{Exists: true, Owned: true, Usable: true}, Issuer: ManagedResourceFacts{Exists: true, Owned: true, Ready: true, Matches: true}, Certificate: ManagedResourceFacts{Exists: true, Owned: true, Ready: true, Matches: true}},
	}
	plan := BuildPreparePlan(validSetup(true), facts, false)
	if !plan.Valid() || !plan.Ready || len(plan.Operations) != 0 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestVerifyAcceptsMaterialIndependentOfRecipe(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	setup := validSetup(false)
	facts := Facts{Certificate: CertificateFacts{Exists: true, Valid: true, KeyMatches: true, DNSNamesCovered: true, NotBefore: now, NotAfter: now.Add(90 * 24 * time.Hour)}}
	if diagnostics := Verify(setup, facts); len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	facts.Certificate = CertificateFacts{Exists: true, Problem: "certificate does not cover *.molejo.dev"}
	if diagnostics := Verify(setup, facts); len(diagnostics) != 1 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
}

func TestSetupNormalizesAndSortsDNSNames(t *testing.T) {
	setup := validSetup(false)
	setup.Spec.DNSNames = []string{"MOLEJO.DEV.", "*.stateful.molejo.dev", "molejo.dev"}
	normalized, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	if len(normalized.Spec.DNSNames) != 2 || normalized.Spec.DNSNames[0] != "*.stateful.molejo.dev" || normalized.Spec.DNSNames[1] != "molejo.dev" {
		t.Fatalf("dnsNames=%v", normalized.Spec.DNSNames)
	}
}

func TestPublishedTLSSetupsMatchContract(t *testing.T) {
	for _, name := range []string{"tls-existing-secret.yaml", "tls-molejo-dev-staging.yaml", "tls-molejo-dev-production.yaml"} {
		setup, err := LoadSetup(filepath.Join("..", "..", "..", "..", "deploy", "examples", name))
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if _, diagnostics := NormalizeAndValidate(setup); len(diagnostics) != 0 {
			t.Fatalf("%s diagnostics=%+v", name, diagnostics)
		}
	}
}

func TestCertificateRecipeUsesUnstructuredJSONTypes(t *testing.T) {
	_, _, _, _, certificate, err := CertManagerResources(validSetup(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, found, nestedErr := unstructured.NestedMap(certificate, "spec"); nestedErr != nil || !found {
		t.Fatalf("certificate spec found=%t err=%v", found, nestedErr)
	}
}

func validSetup(withRecipe bool) Setup {
	setup := Setup{
		APIVersion: APIVersion,
		Kind:       SetupKind,
		Metadata:   Metadata{Name: "molejo-dev"},
		Spec: SetupSpec{
			DNSNames:        []string{"*.molejo.dev", "*.stateful.molejo.dev", "molejo.dev"},
			TargetSecretRef: ObjectReference{Namespace: "molejo-system", Name: "molejo-dev-tls"},
		},
	}
	if withRecipe {
		setup.Spec.Recipe = RecipeSpec{ID: RecipeCertManagerCloudflare, Config: map[string]any{
			"issuer":    map[string]any{"type": "acme", "environment": "staging", "email": "owner@example.com"},
			"challenge": map[string]any{"type": "dns01", "solver": "cloudflare", "credentialSecretRef": map[string]any{"namespace": "cert-manager", "name": "cloudflare-dns-token"}},
		}}
	}
	return setup
}
