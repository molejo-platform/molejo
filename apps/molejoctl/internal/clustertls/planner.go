package clustertls

import (
	"fmt"
	"slices"
)

type Driver interface {
	ID() string
	Plan(Profile, Facts) Plan
}

type Registry struct{ drivers map[string]Driver }

func NewRegistry(drivers ...Driver) Registry {
	registry := Registry{drivers: make(map[string]Driver, len(drivers))}
	for _, driver := range drivers {
		registry.drivers[driver.ID()] = driver
	}
	return registry
}

func (r Registry) Build(profile Profile, facts Facts) Plan {
	normalized, diagnostics := NormalizeAndValidate(profile)
	if len(diagnostics) > 0 {
		return Plan{Diagnostics: diagnostics}
	}
	driver, exists := r.drivers[normalized.Spec.Certificate.Driver]
	if !exists {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.certificate.driver", Message: fmt.Sprintf("driver %q is not registered", normalized.Spec.Certificate.Driver)}}}
	}
	return driver.Plan(normalized, facts)
}

func desiredBinding(profile Profile, facts CertificateFacts, renewal string) Binding {
	return Binding{
		APIVersion: APIVersion,
		Kind:       BindingKind,
		Metadata:   profile.Metadata,
		Spec: BindingSpec{
			Domains:        slices.Clone(profile.Spec.Domains),
			Termination:    "KubernetesSecret",
			CertificateRef: profile.Spec.Certificate.TargetSecretRef,
			Driver:         DriverReference{ID: profile.Spec.Certificate.Driver, Version: "builtin/v1alpha1"},
		},
		Status: BindingStatus{
			NotBefore:  facts.NotBefore,
			NotAfter:   facts.NotAfter,
			Renewal:    renewal,
			Conditions: []Condition{{Type: "CertificateReady", Status: "True"}, {Type: "Ready", Status: "True"}},
		},
	}
}

func bindingMatches(current *Binding, desired Binding) bool {
	if current == nil {
		return false
	}
	return current.APIVersion == desired.APIVersion && current.Kind == desired.Kind &&
		current.Metadata.Name == desired.Metadata.Name && slices.Equal(current.Spec.Domains, desired.Spec.Domains) &&
		current.Spec.Termination == desired.Spec.Termination && current.Spec.CertificateRef == desired.Spec.CertificateRef &&
		current.Spec.Driver == desired.Spec.Driver && current.Status.NotBefore.Equal(desired.Status.NotBefore) &&
		current.Status.NotAfter.Equal(desired.Status.NotAfter) && current.Status.Renewal == desired.Status.Renewal
}

func bindingOperation(binding Binding) Operation {
	return Operation{Kind: OperationWriteBinding, ID: "binding/" + binding.Metadata.Name, Detail: "write TLS binding", Binding: &binding}
}
