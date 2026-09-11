package publication

import (
	"fmt"
	"slices"
)

func BuildPlan(input Setup, current Current) (Setup, Plan) {
	setup, diagnostics := NormalizeAndValidate(input)
	plan := Plan{Diagnostics: diagnostics}
	if len(diagnostics) > 0 {
		return setup, plan
	}
	desiredBinding := setup.Spec.Binding
	bindingID := ""
	if current.Binding != nil {
		bindingID = current.Binding.ID
	}
	if current.Binding == nil || !sameBinding(current.Binding.Spec, desiredBinding) {
		operation := Operation{Kind: EnsureBinding, ID: setup.Spec.ClusterID, Detail: fmt.Sprintf("configure %s/%s with %d HTTPS listeners", desiredBinding.GatewayNamespace, desiredBinding.GatewayName, len(desiredBinding.Listeners))}
		if current.Binding != nil {
			revision := current.Binding.Revision
			operation.IfMatch = &revision
		}
		plan.Operations = append(plan.Operations, operation)
	}
	for i := range setup.Spec.Domains {
		desired := &setup.Spec.Domains[i]
		actual, found := current.Domains[desired.ID]
		if !found || actual.Name != desired.Name || actual.Kind != desired.Kind || !slices.Equal(actual.ReservedNames, desired.ReservedNames) {
			operation := Operation{Kind: EnsureDomain, ID: desired.ID, Detail: fmt.Sprintf("ensure %s domain %s", desired.Kind, desired.Name), Domain: desired}
			if found {
				version := actual.Version
				operation.IfMatch = &version
			}
			plan.Operations = append(plan.Operations, operation)
		}
		for _, workspaceID := range desired.WorkspaceIDs {
			grant, found := current.Grants[GrantKey(desired.ID, workspaceID)]
			if !found || bindingID != "" && grant.BindingID != bindingID {
				plan.Operations = append(plan.Operations, Operation{Kind: EnsureGrant, ID: desired.ID + "/" + workspaceID, Detail: fmt.Sprintf("grant %s to workspace %s", desired.ID, workspaceID), Domain: desired, WorkspaceID: workspaceID})
			}
		}
	}
	// Omission is intentionally non-destructive. Revocation and deletion require
	// their explicit commands so a declarative edit can never prune live access.
	return setup, plan
}

func sameBinding(a, b BindingSpec) bool {
	return a.SchemaVersion == b.SchemaVersion && a.GatewayNamespace == b.GatewayNamespace && a.GatewayName == b.GatewayName && slices.Equal(a.Listeners, b.Listeners)
}
