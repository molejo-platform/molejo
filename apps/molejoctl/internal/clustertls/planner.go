package clustertls

func BuildPreparePlan(setup Setup, facts Facts, credentialAvailable bool) Plan {
	normalized, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return Plan{Diagnostics: diagnostics}
	}
	if normalized.Spec.Recipe.ID != RecipeCertManagerCloudflare {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.recipe.id", Message: "must be cert-manager-cloudflare"}}}
	}
	config, diagnostics := decodeCertManagerConfig(normalized)
	if len(diagnostics) > 0 {
		return Plan{Diagnostics: diagnostics}
	}

	plan := Plan{}
	credential := facts.CertManager.Credential
	credentialRef := config.Challenge.CredentialSecretRef
	if !credential.Exists || !credential.Usable {
		if !credentialAvailable {
			return Plan{Diagnostics: []Diagnostic{{Field: "credential", Message: "credential Secret is unavailable; provide --credential-env"}}}
		}
		if credential.Exists && !credential.Owned {
			return Plan{Diagnostics: []Diagnostic{{Field: "credential", Message: "existing credential Secret is not owned by molejoctl"}}}
		}
		if !facts.CertManager.NamespaceExists {
			plan.Operations = append(plan.Operations, Operation{Kind: OperationEnsureNamespace, ID: "namespace/cert-manager", Detail: "ensure cert-manager namespace", Namespace: credentialRef.Namespace})
		}
		plan.Operations = append(plan.Operations, Operation{Kind: OperationEnsureCredentialSecret, ID: "secret/" + credentialRef.Name, Detail: "ensure Cloudflare DNS credential Secret", Credential: &credentialRef})
	} else if credentialAvailable && !credential.Matches {
		if !credential.Owned {
			return Plan{Diagnostics: []Diagnostic{{Field: "credential", Message: "refusing to replace a credential Secret not owned by molejoctl"}}}
		}
		plan.Operations = append(plan.Operations, Operation{Kind: OperationEnsureCredentialSecret, ID: "secret/" + credentialRef.Name, Detail: "rotate Cloudflare DNS credential Secret", Credential: &credentialRef})
	}

	if !facts.CertManager.Installed {
		plan.Operations = append(plan.Operations, Operation{
			Kind: OperationEnsureHelmRelease, ID: "helm/cert-manager", Detail: "install cert-manager " + CertManagerVersion,
			Helm: &HelmRelease{Name: "cert-manager", Namespace: "cert-manager", Chart: CertManagerChart, Version: CertManagerVersion, Values: map[string]any{"crds": map[string]any{"enabled": true}}},
		})
	} else if !facts.CertManager.VersionMatches {
		return Plan{Diagnostics: []Diagnostic{{Field: "cert-manager", Message: "installed Helm release version differs from the supported recipe"}}}
	}

	if facts.CertManager.Issuer.Exists && !facts.CertManager.Issuer.Owned {
		return Plan{Diagnostics: []Diagnostic{{Field: "ClusterIssuer", Message: "existing issuer is not owned by molejoctl"}}}
	}
	if !facts.CertManager.Issuer.Ready || !facts.CertManager.Issuer.Matches {
		issuer := clusterIssuerObject(normalized, config)
		plan.Operations = append(plan.Operations,
			Operation{Kind: OperationEnsureObject, ID: "clusterissuer/" + issuerName(normalized, config), Detail: "ensure ACME ClusterIssuer", Object: issuer},
			Operation{Kind: OperationWaitForCondition, ID: "clusterissuer-ready/" + issuerName(normalized, config), Detail: "wait for ClusterIssuer Ready", Wait: &WaitTarget{GroupVersionResource: "cert-manager.io/v1/clusterissuers", Name: issuerName(normalized, config), Condition: "Ready"}},
		)
	}

	if facts.CertManager.Certificate.Exists && !facts.CertManager.Certificate.Owned {
		return Plan{Diagnostics: []Diagnostic{{Field: "Certificate", Message: "existing Certificate is not owned by molejoctl"}}}
	}
	if !facts.CertManager.Certificate.Ready || !facts.CertManager.Certificate.Matches || !facts.Certificate.Valid {
		certificate := certificateObject(normalized, config)
		plan.Operations = append(plan.Operations,
			Operation{Kind: OperationEnsureObject, ID: "certificate/" + normalized.Metadata.Name, Detail: "ensure managed Certificate", Object: certificate},
			Operation{Kind: OperationWaitForCondition, ID: "certificate-ready/" + normalized.Metadata.Name, Detail: "wait for Certificate Ready", Wait: &WaitTarget{GroupVersionResource: "cert-manager.io/v1/certificates", Namespace: normalized.Spec.TargetSecretRef.Namespace, Name: certificateName(normalized), Condition: "Ready"}},
		)
		return plan
	}

	plan.Ready = len(plan.Operations) == 0
	return plan
}

func Verify(setup Setup, facts Facts) []Diagnostic {
	_, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return diagnostics
	}
	if !facts.Certificate.Exists {
		return []Diagnostic{{Field: "spec.targetSecretRef", Message: "TLS Secret does not exist"}}
	}
	if !facts.Certificate.Valid {
		return []Diagnostic{{Field: "spec.targetSecretRef", Message: facts.Certificate.Problem}}
	}
	return nil
}
