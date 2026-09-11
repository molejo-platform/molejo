package gateway

import (
	"fmt"
	"regexp"
	"strings"
)

var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

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

	if len(instance.Listeners) < 1 || len(instance.Listeners) > 10 {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.instance.listeners", Message: "must contain between 1 and 10 listeners"})
	}
	names := map[string]bool{}
	hostnames := map[string]bool{}
	instance.Listeners = append([]ListenerSpec(nil), instance.Listeners...)
	for i := range instance.Listeners {
		l := &instance.Listeners[i]
		l.Name = strings.TrimSpace(l.Name)
		l.CertificateSecret.Namespace = strings.TrimSpace(l.CertificateSecret.Namespace)
		l.CertificateSecret.Name = strings.TrimSpace(l.CertificateSecret.Name)
		l.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(l.Hostname), "."))
		field := fmt.Sprintf("spec.gateway.instance.listeners[%d]", i)
		if !dnsLabelPattern.MatchString(l.Name) || len(l.Name) > 63 || names[l.Name] {
			diagnostics = append(diagnostics, Diagnostic{Field: field + ".name", Message: "must be a unique DNS label"})
		}
		names[l.Name] = true
		hostname := strings.TrimPrefix(l.Hostname, "*.")
		valid := len(hostname) <= 253 && hostname != ""
		for _, label := range strings.Split(hostname, ".") {
			valid = valid && len(label) <= 63 && dnsLabelPattern.MatchString(label)
		}
		if !valid || hostnames[l.Hostname] {
			diagnostics = append(diagnostics, Diagnostic{Field: field + ".hostname", Message: "must be a unique exact or wildcard DNS name"})
		}
		hostnames[l.Hostname] = true
		if l.CertificateSecret.Namespace != instance.Namespace {
			diagnostics = append(diagnostics, Diagnostic{Field: field + ".certificateSecret.namespace", Message: "must match the Gateway namespace"})
		}
		if !dnsLabelPattern.MatchString(l.CertificateSecret.Name) || len(l.CertificateSecret.Name) > 63 {
			diagnostics = append(diagnostics, Diagnostic{Field: field + ".certificateSecret.name", Message: "must be a DNS label"})
		}
	}
	return setup, diagnostics
}
