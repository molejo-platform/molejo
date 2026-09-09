package workspaceboundary

import (
	"testing"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestPlanRejectsUnsafePlacements(t *testing.T) {
	tests := []struct {
		name string
		spec platformv1alpha1.WorkspacePlacementSpec
	}{
		{name: "reserved namespace", spec: validSpec("kube-system")},
		{name: "system namespace", spec: validSpec("molejo-system")},
		{name: "identity mismatch", spec: platformv1alpha1.WorkspacePlacementSpec{WorkspaceID: "ws-abcdefghijklmnopqrst", NamespaceName: "ws-bbbbbbbbbbbbbbbbbbbb", AccessProfile: "NamespacedRuntime", LifecycleState: "Ready"}},
		{name: "unknown access profile", spec: withAccessProfile(validSpec("ws-abcdefghijklmnopqrst"), "Admin")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Plan(test.spec); err == nil {
				t.Fatal("unsafe placement was accepted")
			}
		})
	}
}

func TestPlanProducesOnlyFixedBindings(t *testing.T) {
	plan, err := Plan(validSpec("ws-abcdefghijklmnopqrst"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Namespace != "ws-abcdefghijklmnopqrst" || len(plan.Bindings) != 3 {
		t.Fatalf("plan=%+v", plan)
	}
	for _, binding := range plan.Bindings {
		if binding.RoleName == "" || binding.ServiceAccountName == "" || binding.ServiceAccountNamespace != "molejo-system" {
			t.Fatalf("unsafe binding=%+v", binding)
		}
	}
}

func validSpec(namespace string) platformv1alpha1.WorkspacePlacementSpec {
	return platformv1alpha1.WorkspacePlacementSpec{WorkspaceID: "ws-abcdefghijklmnopqrst", NamespaceName: namespace, AccessProfile: "NamespacedRuntime", LifecycleState: "Ready"}
}

func withAccessProfile(spec platformv1alpha1.WorkspacePlacementSpec, profile string) platformv1alpha1.WorkspacePlacementSpec {
	spec.AccessProfile = profile
	return spec
}
