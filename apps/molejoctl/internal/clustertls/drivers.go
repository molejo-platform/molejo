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

func decodeCertManagerConfig(setup Setup) (certManagerConfig, []Diagnostic) {
	var config certManagerConfig
	encoded, err := yaml.Marshal(setup.Spec.Recipe.Config)
	if err == nil {
		err = yaml.Unmarshal(encoded, &config)
	}
	if err != nil {
		return config, []Diagnostic{{Field: "spec.recipe.config", Message: "is invalid: " + err.Error()}}
	}
	diagnostics := []Diagnostic{}
	if config.Issuer.Type != "acme" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.recipe.config.issuer.type", Message: "only acme is supported"})
	}
	if config.Issuer.Environment != "staging" && config.Issuer.Environment != "production" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.recipe.config.issuer.environment", Message: "must be staging or production"})
	}
	if address, parseErr := mail.ParseAddress(config.Issuer.Email); parseErr != nil || address.Address != config.Issuer.Email {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.recipe.config.issuer.email", Message: "must be a valid email address"})
	}
	if config.Challenge.Type != "dns01" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.recipe.config.challenge.type", Message: "only dns01 is supported"})
	}
	if config.Challenge.Solver != "cloudflare" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.recipe.config.challenge.solver", Message: "only cloudflare is supported"})
	}
	if config.Challenge.CredentialSecretRef.Namespace != "cert-manager" || config.Challenge.CredentialSecretRef.Name == "" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.recipe.config.challenge.credentialSecretRef", Message: "must reference a Secret in namespace cert-manager"})
	}
	return config, diagnostics
}

func issuerName(setup Setup, config certManagerConfig) string {
	return "molejo-" + setup.Metadata.Name + "-" + config.Issuer.Environment
}

func certificateName(setup Setup) string { return "molejo-" + setup.Metadata.Name }

func CertManagerResources(setup Setup) (issuerNameValue, certificateNameValue string, credential ObjectReference, issuer, certificate map[string]any, err error) {
	config, diagnostics := decodeCertManagerConfig(setup)
	if len(diagnostics) > 0 {
		return "", "", ObjectReference{}, nil, nil, fmt.Errorf("invalid cert-manager recipe: %s", diagnostics[0].Message)
	}
	return issuerName(setup, config), certificateName(setup), config.Challenge.CredentialSecretRef, clusterIssuerObject(setup, config), certificateObject(setup, config), nil
}

func clusterIssuerObject(setup Setup, config certManagerConfig) map[string]any {
	server := "https://acme-staging-v02.api.letsencrypt.org/directory"
	if config.Issuer.Environment == "production" {
		server = "https://acme-v02.api.letsencrypt.org/directory"
	}
	name := issuerName(setup, config)
	return map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
		"metadata": map[string]any{"name": name, "labels": managedLabels(setup)},
		"spec": map[string]any{"acme": map[string]any{
			"email": config.Issuer.Email, "server": server,
			"privateKeySecretRef": map[string]any{"name": name + "-account-key"},
			"solvers":             []any{map[string]any{"dns01": map[string]any{"cloudflare": map[string]any{"apiTokenSecretRef": map[string]any{"name": config.Challenge.CredentialSecretRef.Name, "key": "api-token"}}}}},
		}},
	}
}

func certificateObject(setup Setup, config certManagerConfig) map[string]any {
	return map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"name": certificateName(setup), "namespace": setup.Spec.TargetSecretRef.Namespace, "labels": managedLabels(setup)},
		"spec": map[string]any{
			"secretName": setup.Spec.TargetSecretRef.Name, "dnsNames": stringsToAny(setup.Spec.DNSNames),
			"secretTemplate": map[string]any{"labels": managedLabels(setup)},
			"issuerRef":      map[string]any{"name": issuerName(setup, config), "kind": "ClusterIssuer", "group": "cert-manager.io"},
			"privateKey":     map[string]any{"algorithm": "ECDSA", "size": int64(256), "rotationPolicy": "Always"},
		},
	}
}

func managedLabels(setup Setup) map[string]any {
	return map[string]any{
		"app.kubernetes.io/managed-by": "molejoctl",
		"config.molejo.dev/tls-setup":  setup.Metadata.Name,
		"config.molejo.dev/tls-recipe": setup.Spec.Recipe.ID,
	}
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
