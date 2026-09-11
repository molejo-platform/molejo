package publication

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

func TestBuildPlanOrdersCreationAndNeverPrunesOmittedState(t *testing.T) {
	setup := validSetup()
	current := Current{
		Domains: map[string]Domain{"old": {ID: "old", Kind: "Exact", Name: "old.example.test", Version: 1}},
		Grants:  map[string]Grant{GrantKey("old", "ws-zzzzzzzzzzzzzzzzzzzz"): {DomainID: "old", WorkspaceID: "ws-zzzzzzzzzzzzzzzzzzzz", BindingID: "old-binding"}},
	}
	_, plan := BuildPlan(setup, current)
	if !plan.Valid() {
		t.Fatalf("unexpected diagnostics: %+v", plan.Diagnostics)
	}
	kinds := make([]OperationKind, 0, len(plan.Operations))
	for _, operation := range plan.Operations {
		kinds = append(kinds, operation.Kind)
	}
	if want := []OperationKind{EnsureBinding, EnsureDomain, EnsureGrant}; !slices.Equal(kinds, want) {
		t.Fatalf("operation order = %v, want %v", kinds, want)
	}
	for _, operation := range plan.Operations {
		if operation.ID == "old" || operation.ID == "old/ws-zzzzzzzzzzzzzzzzzzzz" {
			t.Fatalf("omitted state produced a destructive operation: %+v", operation)
		}
	}
}

func TestBuildPlanReadyWhenDesiredStateMatches(t *testing.T) {
	setup := validSetup()
	current := Current{
		Binding: &Binding{ID: "binding", Revision: 2, Spec: setup.Spec.Binding},
		Domains: map[string]Domain{"home": {ID: "home", Kind: "Exact", Name: "example.test", ReservedNames: []string{}, Version: 1}},
		Grants:  map[string]Grant{GrantKey("home", setup.Spec.Domains[0].WorkspaceIDs[0]): {DomainID: "home", WorkspaceID: setup.Spec.Domains[0].WorkspaceIDs[0], BindingID: "binding"}},
	}
	_, plan := BuildPlan(setup, current)
	if !plan.Ready() {
		t.Fatalf("matching state did not converge: %+v", plan)
	}
}

func TestValidationRejectsUncoveredDomainAndBoundsBeforeIO(t *testing.T) {
	setup := validSetup()
	setup.Spec.Domains[0].Name = "other.test"
	for range 100 {
		setup.Spec.Domains = append(setup.Spec.Domains, DomainSpec{ID: "extra", Kind: "Exact", Name: "example.test"})
	}
	_, diagnostics := NormalizeAndValidate(setup)
	codes := []string{}
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	if !slices.Contains(codes, "listener_required") || !slices.Contains(codes, "limit") {
		t.Fatalf("missing bounded validation diagnostics: %+v", diagnostics)
	}
}

func TestMutationStateMachineRequiresObservationAfterUnknownResponse(t *testing.T) {
	unknown := CompleteMutation(errors.New("connection reset"), true)
	if unknown.State != MutationResponseUnknown {
		t.Fatalf("state = %s", unknown.State)
	}
	if recovered := RecoverMutation(true, true, nil); recovered.State != MutationRecovered {
		t.Fatalf("equal observation did not recover: %+v", recovered)
	}
	if conflict := RecoverMutation(false, true, nil); conflict.State != MutationConflict {
		t.Fatalf("different observation did not conflict: %+v", conflict)
	}
}

func TestLoadRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown":  "apiVersion: config.molejo.dev/v1alpha1\nkind: HTTPPublicationSetup\nmetadata: {name: test}\nspec: {clusterId: cls-abcdefghijklmnopqrst, unexpected: true}\n",
		"multiple": "apiVersion: config.molejo.dev/v1alpha1\nkind: HTTPPublicationSetup\nmetadata: {name: test}\nspec: {}\n---\n{}\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "setup.yaml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid setup was accepted")
			}
		})
	}
}

func validSetup() Setup {
	return Setup{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "example"},
		Spec: SetupSpec{
			ClusterID: "cls-abcdefghijklmnopqrst",
			Binding: BindingSpec{
				SchemaVersion:    kubernetesbinding.HTTPBindingSchemaVersion,
				GatewayNamespace: "molejo-system",
				GatewayName:      "molejo",
				Listeners:        []kubernetesbinding.HTTPListener{{Name: "https-apex", Hostname: "example.test"}},
			},
			Domains: []DomainSpec{{ID: "home", Kind: "Exact", Name: "example.test", ReservedNames: []string{}, WorkspaceIDs: []string{"ws-abcdefghijklmnopqrst"}}},
		},
	}
}
