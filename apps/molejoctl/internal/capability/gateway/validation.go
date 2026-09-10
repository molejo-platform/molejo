package gateway

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	dnsNamePattern  = regexp.MustCompile(`^(?:[a-z0-9](?:[-a-z0-9]*[a-z0-9])?\.)+[a-z]{2,63}$`)
)

func NormalizeAndValidate(setup Setup) (Setup, []Diagnostic) {
	setup.APIVersion = strings.TrimSpace(setup.APIVersion)
	setup.Kind = strings.TrimSpace(setup.Kind)
	setup.Metadata.Name = strings.TrimSpace(setup.Metadata.Name)
	setup.Spec.Profile = strings.TrimSpace(setup.Spec.Profile)
	controller := &setup.Spec.Gateway.Controller
	controller.Management = strings.TrimSpace(controller.Management)
	controller.Name = strings.TrimSpace(controller.Name)
	controller.Version = strings.TrimSpace(controller.Version)
	controller.Namespace = strings.TrimSpace(controller.Namespace)
	controller.ClassName = strings.TrimSpace(controller.ClassName)
	setup.Spec.Gateway.Service.Type = strings.TrimSpace(setup.Spec.Gateway.Service.Type)
	instance := &setup.Spec.Gateway.Instance
	instance.Namespace = strings.TrimSpace(instance.Namespace)
	instance.Name = strings.TrimSpace(instance.Name)
	instance.HTTPSListener = strings.TrimSpace(instance.HTTPSListener)
	instance.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(instance.Hostname), "."))
	instance.CertificateSecret.Namespace = strings.TrimSpace(instance.CertificateSecret.Namespace)
	instance.CertificateSecret.Name = strings.TrimSpace(instance.CertificateSecret.Name)

	diagnostics := []Diagnostic{}
	if setup.APIVersion != APIVersion {
		diagnostics = append(diagnostics, Diagnostic{Field: "apiVersion", Message: fmt.Sprintf("must be %s", APIVersion)})
	}
	if setup.Kind != Kind {
		diagnostics = append(diagnostics, Diagnostic{Field: "kind", Message: fmt.Sprintf("must be %s", Kind)})
	}
	labels := []struct{ field, value string }{
		{"metadata.name", setup.Metadata.Name},
		{"spec.gateway.controller.namespace", controller.Namespace},
		{"spec.gateway.controller.className", controller.ClassName},
		{"spec.gateway.instance.namespace", instance.Namespace},
		{"spec.gateway.instance.name", instance.Name},
		{"spec.gateway.instance.httpsListener", instance.HTTPSListener},
		{"spec.gateway.instance.certificateSecret.name", instance.CertificateSecret.Name},
	}
	for _, label := range labels {
		if len(label.value) > 63 || !dnsLabelPattern.MatchString(label.value) {
			diagnostics = append(diagnostics, Diagnostic{Field: label.field, Message: "must be a lowercase DNS label with at most 63 characters"})
		}
	}
	if setup.Spec.Profile != ProfileK3s {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.profile", Message: "only k3s is supported"})
	}
	if controller.Management != ControllerManaged || controller.Name != ControllerTraefik {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.controller", Message: "only managed Traefik is supported"})
	}
	if controller.Version != TraefikVersion {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.controller.version", Message: fmt.Sprintf("must be %s", TraefikVersion)})
	}
	if setup.Spec.Gateway.Service.Type != "NodePort" {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.service.type", Message: "must be NodePort for the k3s profile"})
	}
	ports := []struct {
		field string
		port  int32
	}{{"httpNodePort", setup.Spec.Gateway.Service.HTTPNodePort}, {"httpsNodePort", setup.Spec.Gateway.Service.HTTPSNodePort}}
	for _, candidate := range ports {
		if candidate.port < 30000 || candidate.port > 32767 {
			diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.service." + candidate.field, Message: "must be between 30000 and 32767"})
		}
	}
	if setup.Spec.Gateway.Service.HTTPNodePort == setup.Spec.Gateway.Service.HTTPSNodePort {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.service", Message: "HTTP and HTTPS NodePorts must differ"})
	}
	hostname := strings.TrimPrefix(instance.Hostname, "*.")
	if !strings.HasPrefix(instance.Hostname, "*.") || !dnsNamePattern.MatchString(hostname) {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.instance.hostname", Message: "must be a single-label wildcard DNS name"})
	}
	if instance.CertificateSecret.Namespace != instance.Namespace {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.instance.certificateSecret.namespace", Message: "must match the Gateway namespace"})
	}
	return setup, diagnostics
}
