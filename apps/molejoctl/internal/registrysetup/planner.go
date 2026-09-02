package registrysetup

func BuildPlan(setup Setup, facts Facts) Plan {
	diagnostics := []Diagnostic{}
	if !facts.NamespaceExists {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.target.namespace", Message: "namespace does not exist; the runbook does not create namespaces"})
	}
	if facts.NamespaceExists && !facts.ServiceAccountExists {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.target.serviceAccount", Message: "ServiceAccount does not exist; the runbook does not create ServiceAccounts"})
	}
	if facts.Secret.Exists && !facts.Secret.Owned {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.authentication.secretName", Message: "existing Secret is not owned by this registry setup"})
	}
	if len(diagnostics) > 0 {
		return Plan{Diagnostics: diagnostics}
	}
	plan := Plan{}
	if !facts.Secret.Exists || !facts.Secret.Valid || !facts.Secret.Matches {
		plan.Operations = append(plan.Operations, Operation{
			Kind:   OperationEnsureSecret,
			ID:     "secret/" + setup.Spec.Target.Namespace + "/" + setup.Spec.Authentication.SecretName,
			Detail: "ensure pull Secret " + setup.Spec.Target.Namespace + "/" + setup.Spec.Authentication.SecretName,
		})
	}
	if !facts.PullSecretAttached {
		plan.Operations = append(plan.Operations, Operation{
			Kind:   OperationAttachPullSecret,
			ID:     "serviceaccount/" + setup.Spec.Target.Namespace + "/" + setup.Spec.Target.ServiceAccount,
			Detail: "attach pull Secret to ServiceAccount " + setup.Spec.Target.Namespace + "/" + setup.Spec.Target.ServiceAccount,
		})
	}
	plan.Ready = len(plan.Operations) == 0
	return plan
}

func Verify(facts Facts) []Diagnostic {
	diagnostics := []Diagnostic{}
	if !facts.NamespaceExists {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.target.namespace", Message: "namespace does not exist"})
	}
	if !facts.ServiceAccountExists {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.target.serviceAccount", Message: "ServiceAccount does not exist"})
	}
	if !facts.Secret.Exists {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.authentication.secretName", Message: "pull Secret does not exist"})
	} else {
		if !facts.Secret.Owned {
			diagnostics = append(diagnostics, Diagnostic{Field: "spec.authentication.secretName", Message: "pull Secret is not owned by this registry setup"})
		}
		if !facts.Secret.Valid {
			diagnostics = append(diagnostics, Diagnostic{Field: "spec.authentication.secretName", Message: "pull Secret is not a valid Docker config for the configured registry"})
		}
	}
	if !facts.PullSecretAttached {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.target.serviceAccount", Message: "pull Secret is not attached"})
	}
	return diagnostics
}

func MergeImagePullSecrets(current []string, desired string) ([]string, bool) {
	result := append([]string(nil), current...)
	for _, name := range current {
		if name == desired {
			return result, false
		}
	}
	return append(result, desired), true
}
