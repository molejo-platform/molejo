package clustertls

import (
	"fmt"
	"net/mail"

	"gopkg.in/yaml.v3"
)

const (
	CertManagerChart   = "oci://quay.io/jetstack/charts/cert-manager"
	CertManagerVersion = "v1.21.1"
)

type ExistingSecretDriver struct{}

func (ExistingSecretDriver) ID() string { return DriverExistingSecret }

func (ExistingSecretDriver) Plan(profile Profile, facts Facts) Plan {
	if profile.Spec.Management != ManagementExternal {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.management", Message: "existing-secret requires External management"}}}
	}
	if !facts.Certificate.Exists {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.certificate.targetSecretRef", Message: "TLS Secret does not exist"}}}
	}
	if !facts.Certificate.Valid {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.certificate.targetSecretRef", Message: facts.Certificate.Problem}}}
	}
	binding := desiredBinding(profile, facts.Certificate, "External")
	plan := Plan{Binding: &binding}
	if !bindingMatches(facts.Binding, binding) {
		plan.Operations = append(plan.Operations, bindingOperation(binding))
	}
	return plan
}

type CertManagerDriver struct{}

func (CertManagerDriver) ID() string { return DriverCertManager }

type certManagerConfig struct {
	Issuer struct {
		Type        string `yaml:"type"`
		Environment string `yaml:"environment"`
		Email       string `yaml:"email"`
	} `yaml:"issuer"`
	Challenge struct {
		Type                string          `yaml:"type"`
		Solver              string          `yaml:"solver"`
		CredentialSecretRef ObjectReference `yaml:"credentialSecretRef"`
	} `yaml:"challenge"`
}

func (CertManagerDriver) Plan(profile Profile, facts Facts) Plan {
	config, diagnostics := decodeCertManagerConfig(profile)
	if len(diagnostics) > 0 {
		return Plan{Diagnostics: diagnostics}
	}
	if profile.Spec.Management != ManagementManaged {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.management", Message: "cert-manager requires Managed management"}}}
	}
	if !facts.CertManager.CredentialExists {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.certificate.config.challenge.credentialSecretRef", Message: "credential Secret does not exist"}}}
	}

	plan := Plan{}
	if !facts.CertManager.Installed {
		plan.Operations = append(plan.Operations, Operation{
			Kind: OperationEnsureHelmRelease, ID: "helm/cert-manager", Detail: "install cert-manager " + CertManagerVersion,
			Helm: &HelmRelease{Name: "cert-manager", Namespace: "cert-manager", Chart: CertManagerChart, Version: CertManagerVersion, Values: map[string]any{"crds": map[string]any{"enabled": true}}},
		})
	}
	if facts.CertManager.Issuer.Exists && !facts.CertManager.Issuer.Owned {
		plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Field: "ClusterIssuer", Message: "existing issuer is not owned by molejoctl"})
		return plan
	}
	if !facts.CertManager.Issuer.Ready {
		issuer := clusterIssuerObject(profile, config)
		plan.Operations = append(plan.Operations,
			Operation{Kind: OperationEnsureObject, ID: "clusterissuer/" + issuerName(profile, config), Detail: "ensure ACME ClusterIssuer", Object: issuer},
			Operation{Kind: OperationWaitForCondition, ID: "clusterissuer-ready/" + issuerName(profile, config), Detail: "wait for ClusterIssuer Ready", Wait: &WaitTarget{GroupVersionResource: "cert-manager.io/v1/clusterissuers", Name: issuerName(profile, config), Condition: "Ready"}},
		)
	}
	if facts.CertManager.Certificate.Exists && !facts.CertManager.Certificate.Owned {
		plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Field: "Certificate", Message: "existing Certificate is not owned by molejoctl"})
		return plan
	}
	if !facts.CertManager.Certificate.Ready || !facts.Certificate.Valid {
		certificate := certificateObject(profile, config)
		plan.Operations = append(plan.Operations,
			Operation{Kind: OperationEnsureObject, ID: "certificate/" + profile.Metadata.Name, Detail: "ensure managed Certificate", Object: certificate},
			Operation{Kind: OperationWaitForCondition, ID: "certificate-ready/" + profile.Metadata.Name, Detail: "wait for Certificate Ready", Wait: &WaitTarget{GroupVersionResource: "cert-manager.io/v1/certificates", Namespace: profile.Spec.Certificate.TargetSecretRef.Namespace, Name: certificateName(profile), Condition: "Ready"}},
		)
		return plan
	}
	binding := desiredBinding(profile, facts.Certificate, "Automatic")
	plan.Binding = &binding
	if !bindingMatches(facts.Binding, binding) {
		plan.Operations = append(plan.Operations, bindingOperation(binding))
	}
	return plan
}

func decodeCertManagerConfig(profile Profile) (certManagerConfig, []Diagnostic) {
	var config certManagerConfig
	encoded, err := yaml.Marshal(profile.Spec.Certificate.Config)
	if err == nil {
		err = yaml.Unmarshal(encoded, &config)
	}
	if err != nil {
		return config, []Diagnostic{{Field: "spec.certificate.config", Message: "is invalid: " + err.Error()}}
	}
	diagnostics := []Diagnostic{}
	if config.Issuer.Type != "acme" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.config.issuer.type", Message: "only acme is supported"})
	}
	if config.Issuer.Environment != "staging" && config.Issuer.Environment != "production" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.config.issuer.environment", Message: "must be staging or production"})
	}
	if address, parseErr := mail.ParseAddress(config.Issuer.Email); parseErr != nil || address.Address != config.Issuer.Email {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.config.issuer.email", Message: "must be a valid email address"})
	}
	if config.Challenge.Type != "dns01" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.config.challenge.type", Message: "only dns01 is supported"})
	}
	if config.Challenge.Solver != "cloudflare" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.config.challenge.solver", Message: "only cloudflare is supported"})
	}
	if config.Challenge.CredentialSecretRef.Namespace != "cert-manager" || config.Challenge.CredentialSecretRef.Name == "" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.config.challenge.credentialSecretRef", Message: "must reference a Secret in namespace cert-manager"})
	}
	return config, diagnostics
}

func issuerName(profile Profile, config certManagerConfig) string {
	return "molejo-" + profile.Metadata.Name + "-" + config.Issuer.Environment
}

func certificateName(profile Profile) string { return "molejo-" + profile.Metadata.Name }

func CertManagerResourceNames(profile Profile) (issuer, certificate string, credential ObjectReference, err error) {
	config, diagnostics := decodeCertManagerConfig(profile)
	if len(diagnostics) > 0 {
		return "", "", ObjectReference{}, fmt.Errorf("invalid cert-manager configuration: %s", diagnostics[0].Message)
	}
	return issuerName(profile, config), certificateName(profile), config.Challenge.CredentialSecretRef, nil
}

func clusterIssuerObject(profile Profile, config certManagerConfig) map[string]any {
	server := "https://acme-staging-v02.api.letsencrypt.org/directory"
	if config.Issuer.Environment == "production" {
		server = "https://acme-v02.api.letsencrypt.org/directory"
	}
	name := issuerName(profile, config)
	return map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
		"metadata": map[string]any{"name": name, "labels": managedLabels(profile, DriverCertManager)},
		"spec": map[string]any{"acme": map[string]any{
			"email": config.Issuer.Email, "server": server,
			"privateKeySecretRef": map[string]any{"name": name + "-account-key"},
			"solvers":             []any{map[string]any{"dns01": map[string]any{"cloudflare": map[string]any{"apiTokenSecretRef": map[string]any{"name": config.Challenge.CredentialSecretRef.Name, "key": "api-token"}}}}},
		}},
	}
}

func certificateObject(profile Profile, config certManagerConfig) map[string]any {
	return map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"name": certificateName(profile), "namespace": profile.Spec.Certificate.TargetSecretRef.Namespace, "labels": managedLabels(profile, DriverCertManager)},
		"spec": map[string]any{
			"secretName":     profile.Spec.Certificate.TargetSecretRef.Name,
			"dnsNames":       stringsToAny(profile.Spec.Domains),
			"secretTemplate": map[string]any{"labels": managedLabels(profile, DriverCertManager)},
			"issuerRef":      map[string]any{"name": issuerName(profile, config), "kind": "ClusterIssuer", "group": "cert-manager.io"},
			"privateKey":     map[string]any{"algorithm": "ECDSA", "size": 256, "rotationPolicy": "Always"},
		},
	}
}

func managedLabels(profile Profile, driver string) map[string]any {
	return map[string]any{
		"app.kubernetes.io/managed-by":    "molejoctl",
		"platform.molejo.dev/tls-profile": profile.Metadata.Name,
		"platform.molejo.dev/tls-driver":  driver,
	}
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
