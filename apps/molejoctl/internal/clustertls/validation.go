package clustertls

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	setupNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	dnsNamePattern   = regexp.MustCompile(`^(?:[a-z0-9](?:[-a-z0-9]*[a-z0-9])?\.)+[a-z]{2,63}$`)
)

func NormalizeAndValidate(setup Setup) (Setup, []Diagnostic) {
	setup.APIVersion = strings.TrimSpace(setup.APIVersion)
	setup.Kind = strings.TrimSpace(setup.Kind)
	setup.Metadata.Name = strings.TrimSpace(setup.Metadata.Name)
	setup.Spec.TargetSecretRef.Namespace = strings.TrimSpace(setup.Spec.TargetSecretRef.Namespace)
	setup.Spec.TargetSecretRef.Name = strings.TrimSpace(setup.Spec.TargetSecretRef.Name)
	setup.Spec.Recipe.ID = strings.TrimSpace(setup.Spec.Recipe.ID)

	diagnostics := []Diagnostic{}
	if setup.APIVersion != APIVersion {
		diagnostics = append(diagnostics, Diagnostic{Field: "apiVersion", Message: fmt.Sprintf("must be %s", APIVersion)})
	}
	if setup.Kind != SetupKind {
		diagnostics = append(diagnostics, Diagnostic{Field: "kind", Message: fmt.Sprintf("must be %s", SetupKind)})
	}
	if len(setup.Metadata.Name) > 40 || !setupNamePattern.MatchString(setup.Metadata.Name) {
		diagnostics = append(diagnostics, Diagnostic{Field: "metadata.name", Message: "must be a lowercase DNS label with at most 40 characters"})
	}
	if setup.Spec.TargetSecretRef.Namespace == "" || setup.Spec.TargetSecretRef.Name == "" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.targetSecretRef", Message: "namespace and name are required"})
	}

	seen := map[string]struct{}{}
	dnsNames := make([]string, 0, len(setup.Spec.DNSNames))
	for index, raw := range setup.Spec.DNSNames {
		dnsName := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		candidate := strings.TrimPrefix(dnsName, "*.")
		if strings.Contains(candidate, "*") || !dnsNamePattern.MatchString(candidate) {
			diagnostics = append(diagnostics, Diagnostic{Field: fmt.Sprintf("spec.dnsNames[%d]", index), Message: "must be a DNS name or a single-label wildcard"})
			continue
		}
		if _, exists := seen[dnsName]; exists {
			continue
		}
		seen[dnsName] = struct{}{}
		dnsNames = append(dnsNames, dnsName)
	}
	if len(dnsNames) == 0 {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.dnsNames", Message: "must contain at least one valid DNS name"})
	}
	sort.Strings(dnsNames)
	setup.Spec.DNSNames = dnsNames
	return setup, diagnostics
}
