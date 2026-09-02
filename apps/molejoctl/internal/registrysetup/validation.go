package registrysetup

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	validation "k8s.io/apimachinery/pkg/util/validation"
)

var (
	registryHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[0-9]{1,5})?$`)
	digestPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
)

func NormalizeAndValidate(setup Setup) (Setup, []Diagnostic) {
	setup.APIVersion = strings.TrimSpace(setup.APIVersion)
	setup.Kind = strings.TrimSpace(setup.Kind)
	setup.Metadata.Name = strings.TrimSpace(setup.Metadata.Name)
	setup.Spec.Registry.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(setup.Spec.Registry.Host), "."))
	setup.Spec.Authentication.Mode = strings.TrimSpace(setup.Spec.Authentication.Mode)
	setup.Spec.Authentication.SecretName = strings.TrimSpace(setup.Spec.Authentication.SecretName)
	setup.Spec.Target.Namespace = strings.TrimSpace(setup.Spec.Target.Namespace)
	setup.Spec.Target.ServiceAccount = strings.TrimSpace(setup.Spec.Target.ServiceAccount)
	if setup.Spec.Target.ServiceAccount == "" {
		setup.Spec.Target.ServiceAccount = "default"
	}
	setup.Spec.Probe.Image = strings.TrimSpace(setup.Spec.Probe.Image)

	diagnostics := []Diagnostic{}
	if setup.APIVersion != APIVersion {
		diagnostics = append(diagnostics, Diagnostic{Field: "apiVersion", Message: fmt.Sprintf("must be %s", APIVersion)})
	}
	if setup.Kind != Kind {
		diagnostics = append(diagnostics, Diagnostic{Field: "kind", Message: fmt.Sprintf("must be %s", Kind)})
	}
	for _, candidate := range []struct {
		field string
		value string
	}{
		{"metadata.name", setup.Metadata.Name},
		{"spec.authentication.secretName", setup.Spec.Authentication.SecretName},
		{"spec.target.namespace", setup.Spec.Target.Namespace},
		{"spec.target.serviceAccount", setup.Spec.Target.ServiceAccount},
	} {
		if problems := validation.IsDNS1123Subdomain(candidate.value); len(problems) > 0 {
			diagnostics = append(diagnostics, Diagnostic{Field: candidate.field, Message: "must be a valid lowercase Kubernetes name"})
		}
	}
	if setup.Spec.Authentication.Mode != ModeManagedPullSecret {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.authentication.mode", Message: fmt.Sprintf("must be %s", ModeManagedPullSecret)})
	}
	if !validRegistryHost(setup.Spec.Registry.Host) {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.registry.host", Message: "must be a registry hostname without scheme, path, query, or user information"})
	}
	if !digestPattern.MatchString(setup.Spec.Probe.Image) || !strings.HasPrefix(setup.Spec.Probe.Image, setup.Spec.Registry.Host+"/") {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.probe.image", Message: "must use the configured registry and an immutable sha256 digest"})
	}
	return setup, diagnostics
}

func validRegistryHost(host string) bool {
	if !registryHostPattern.MatchString(host) {
		return false
	}
	colon := strings.LastIndexByte(host, ':')
	if colon < 0 {
		return true
	}
	port, err := strconv.Atoi(host[colon+1:])
	return err == nil && port > 0 && port <= 65535
}
