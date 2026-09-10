package registry

import (
	"strings"
	"testing"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func validSetup() Setup {
	return Initial(InitialOptions{
		Name: "application-images", Host: "registry.molejo.dev", SecretName: "molejo-application-registry",
		Namespace: "molejo-registry-e2e", ServiceAccount: "default",
		ProbeImage: "registry.molejo.dev/molejo/testkit@" + testDigest,
	})
}

func TestNormalizeAndValidateRegistrySetup(t *testing.T) {
	setup, diagnostics := NormalizeAndValidate(validSetup())
	if len(diagnostics) != 0 || setup.Spec.Registry.Host != "registry.molejo.dev" {
		t.Fatalf("setup=%+v diagnostics=%+v", setup, diagnostics)
	}

	tests := []struct {
		name   string
		mutate func(*Setup)
		field  string
	}{
		{name: "scheme", mutate: func(setup *Setup) { setup.Spec.Registry.Host = "https://registry.molejo.dev" }, field: "spec.registry.host"},
		{name: "path", mutate: func(setup *Setup) { setup.Spec.Registry.Host = "registry.molejo.dev/v2" }, field: "spec.registry.host"},
		{name: "mutable image", mutate: func(setup *Setup) { setup.Spec.Probe.Image = "registry.molejo.dev/molejo/testkit:latest" }, field: "spec.probe.image"},
		{name: "other registry", mutate: func(setup *Setup) { setup.Spec.Probe.Image = "ghcr.io/molejo-platform/testkit@" + testDigest }, field: "spec.probe.image"},
		{name: "unknown mode", mutate: func(setup *Setup) { setup.Spec.Authentication.Mode = "Ambient" }, field: "spec.authentication.mode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setup := validSetup()
			test.mutate(&setup)
			_, diagnostics := NormalizeAndValidate(setup)
			if !containsDiagnostic(diagnostics, test.field) {
				t.Fatalf("diagnostics=%+v", diagnostics)
			}
		})
	}
}

func TestBuildPlanIsDeterministicAndIdempotent(t *testing.T) {
	setup := validSetup()
	plan := BuildPlan(setup, Facts{NamespaceExists: true, ServiceAccountExists: true})
	if !plan.Valid() || plan.Ready || len(plan.Operations) != 2 || plan.Operations[0].Kind != OperationEnsureSecret || plan.Operations[1].Kind != OperationAttachPullSecret {
		t.Fatalf("plan=%+v", plan)
	}

	ready := BuildPlan(setup, Facts{
		NamespaceExists: true, ServiceAccountExists: true,
		Secret: SecretFacts{Exists: true, Owned: true, Valid: true, Matches: true}, PullSecretAttached: true,
	})
	if !ready.Ready || len(ready.Operations) != 0 {
		t.Fatalf("ready plan=%+v", ready)
	}
}

func TestBuildPlanRefusesMissingTargetsAndForeignSecret(t *testing.T) {
	setup := validSetup()
	tests := []Facts{
		{},
		{NamespaceExists: true},
		{NamespaceExists: true, ServiceAccountExists: true, Secret: SecretFacts{Exists: true}},
	}
	for _, facts := range tests {
		plan := BuildPlan(setup, facts)
		if plan.Valid() || len(plan.Diagnostics) == 0 || len(plan.Operations) != 0 {
			t.Fatalf("facts=%+v plan=%+v", facts, plan)
		}
	}
}

func TestMergeImagePullSecretsPreservesExternalEntries(t *testing.T) {
	merged, changed := MergeImagePullSecrets([]string{"external"}, "molejo-application-registry")
	if !changed || strings.Join(merged, ",") != "external,molejo-application-registry" {
		t.Fatalf("merged=%v changed=%t", merged, changed)
	}
	again, changed := MergeImagePullSecrets(merged, "molejo-application-registry")
	if changed || strings.Join(again, ",") != "external,molejo-application-registry" {
		t.Fatalf("again=%v changed=%t", again, changed)
	}
}

func containsDiagnostic(diagnostics []Diagnostic, field string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Field == field {
			return true
		}
	}
	return false
}
