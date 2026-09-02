package clustertls

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	profileNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	dnsNamePattern     = regexp.MustCompile(`^(?:[a-z0-9](?:[-a-z0-9]*[a-z0-9])?\.)+[a-z]{2,63}$`)
)

func NormalizeAndValidate(profile Profile) (Profile, []Diagnostic) {
	profile.APIVersion = strings.TrimSpace(profile.APIVersion)
	profile.Kind = strings.TrimSpace(profile.Kind)
	profile.Metadata.Name = strings.TrimSpace(profile.Metadata.Name)
	profile.Spec.Management = strings.TrimSpace(profile.Spec.Management)
	profile.Spec.Certificate.Driver = strings.TrimSpace(profile.Spec.Certificate.Driver)
	profile.Spec.Certificate.TargetSecretRef.Namespace = strings.TrimSpace(profile.Spec.Certificate.TargetSecretRef.Namespace)
	profile.Spec.Certificate.TargetSecretRef.Name = strings.TrimSpace(profile.Spec.Certificate.TargetSecretRef.Name)

	diagnostics := []Diagnostic{}
	if profile.APIVersion != APIVersion {
		diagnostics = append(diagnostics, Diagnostic{Field: "apiVersion", Message: fmt.Sprintf("must be %s", APIVersion)})
	}
	if profile.Kind != ProfileKind {
		diagnostics = append(diagnostics, Diagnostic{Field: "kind", Message: fmt.Sprintf("must be %s", ProfileKind)})
	}
	if len(profile.Metadata.Name) > 40 || !profileNamePattern.MatchString(profile.Metadata.Name) {
		diagnostics = append(diagnostics, Diagnostic{Field: "metadata.name", Message: "must be a lowercase DNS label with at most 40 characters"})
	}
	if profile.Spec.Management != ManagementManaged && profile.Spec.Management != ManagementExternal {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.management", Message: "must be Managed or External"})
	}
	if profile.Spec.Certificate.Driver == "" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.driver", Message: "is required"})
	}
	if profile.Spec.Certificate.TargetSecretRef.Namespace == "" || profile.Spec.Certificate.TargetSecretRef.Name == "" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.certificate.targetSecretRef", Message: "namespace and name are required"})
	}

	seen := map[string]struct{}{}
	domains := make([]string, 0, len(profile.Spec.Domains))
	for index, raw := range profile.Spec.Domains {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		candidate := strings.TrimPrefix(domain, "*.")
		if strings.Contains(candidate, "*") || !dnsNamePattern.MatchString(candidate) {
			diagnostics = append(diagnostics, Diagnostic{Field: fmt.Sprintf("spec.domains[%d]", index), Message: "must be a DNS name or a single-label wildcard"})
			continue
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	if len(domains) == 0 {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.domains", Message: "must contain at least one valid domain"})
	}
	sort.Strings(domains)
	profile.Spec.Domains = domains
	return profile, diagnostics
}
