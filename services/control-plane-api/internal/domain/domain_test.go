package domain

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func validIntent() Intent {
	return Intent{Image: "ghcr.io/example/demo@sha256:" + strings.Repeat("a", 64), Replicas: 1, Ports: []RuntimePort{{Name: "http", ContainerPort: 8080, Protocol: PortProtocolTCP}}, Resources: Resources{Requests: ResourceValues{CPUMillis: 50, MemoryMiB: 64}, Limits: ResourceValues{CPUMillis: 100, MemoryMiB: 128}}, Probes: Probes{Startup: Probe{Type: ProbeHTTP, PortName: "http", Path: "/readyz"}, Liveness: Probe{Type: ProbeHTTP, PortName: "http", Path: "/healthz"}, Readiness: Probe{Type: ProbeHTTP, PortName: "http", Path: "/readyz"}}, PublicEndpoints: []PublicEndpoint{}, Variables: []Variable{}}
}

func TestNormalizeIntentMigratesLegacyPortAndHTTPProbes(t *testing.T) {
	intent := NormalizeIntent(Intent{
		Port:   8080,
		Probes: Probes{Readiness: Probe{Path: "/readyz"}, Liveness: Probe{Path: "/healthz"}},
	})
	if len(intent.Ports) != 1 || intent.Ports[0].Name != "http" {
		t.Fatalf("ports=%+v", intent.Ports)
	}
	for name, probe := range map[string]Probe{"startup": intent.Probes.Startup, "readiness": intent.Probes.Readiness, "liveness": intent.Probes.Liveness} {
		if probe.Type != ProbeHTTP || probe.PortName != "http" || probe.Path == "" {
			t.Fatalf("%s probe=%+v", name, probe)
		}
	}
}

func TestValidateIntent(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Intent)
		wantErr bool
	}{{"valid", func(i *Intent) {}, false}, {"mutable tag", func(i *Intent) { i.Image = "ghcr.io/example/demo:latest" }, true}, {"request above limit", func(i *Intent) { i.Resources.Requests.CPUMillis = 101 }, true}, {"unknown probe port", func(i *Intent) { i.Probes.Readiness.PortName = "admin" }, true}, {"public HTTP endpoint", func(i *Intent) {
		i.PublicEndpoints = []PublicEndpoint{{Name: "web", Type: EndpointHTTP, PortName: "http", Addresses: []HTTPAssociation{{DomainID: "default", BindingID: "pbd-test", Label: "demo"}}}}
	}, false}, {"invalid probe", func(i *Intent) { i.Probes.Readiness.Path = "ready" }, true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := validIntent()
			tt.mutate(&intent)
			if got := ValidateIntent(intent, 5, 2000, 2048); (got != nil) != tt.wantErr {
				t.Fatalf("ValidateIntent() error=%v wantErr=%v", got, tt.wantErr)
			}
		})
	}
}

func TestNormalizeIntentAppliesOnlyStableDefaults(t *testing.T) {
	tests := []struct {
		name         string
		intent       Intent
		wantReplicas int32
		wantPortsNil bool
	}{
		{name: "defaults", intent: Intent{}, wantReplicas: 1},
		{name: "explicit values", intent: Intent{Replicas: 3}, wantReplicas: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeIntent(tt.intent)
			if got.Replicas != tt.wantReplicas || got.Ports == nil || got.PublicEndpoints == nil {
				t.Fatalf("NormalizeIntent() = %+v, want replicas=%d and non-nil collections", got, tt.wantReplicas)
			}
		})
	}
}

func TestNormalizeRuntimeConfigDefaultsPublicationDomain(t *testing.T) {
	configuration := ConfigurationFromIntent(validIntent())
	configuration.PublicEndpoints = []PublicEndpoint{{Name: "web", Type: EndpointTCP, PortName: "http", HostnameLabel: "demo"}}
	normalized := NormalizeRuntimeConfig(configuration)
	if normalized.PublicEndpoints[0].DomainID != "default" {
		t.Fatalf("domainId=%q, want default", normalized.PublicEndpoints[0].DomainID)
	}
}

func TestValidateIntentDoesNotSilentlyApplyDefaults(t *testing.T) {
	intent := validIntent()
	intent.Replicas = 0
	intent.Ports = nil
	if err := ValidateIntent(intent, 5, 2000, 2048); err == nil {
		t.Fatal("ValidateIntent accepted omitted values instead of requiring explicit normalization")
	}
}

func TestOpenAPIRuntimeConfigurationConstraintsMatchDomainBoundaries(t *testing.T) {
	type property struct {
		Pattern              string              `yaml:"pattern"`
		Minimum              *int64              `yaml:"minimum"`
		Maximum              *int64              `yaml:"maximum"`
		MaxLength            *int                `yaml:"maxLength"`
		Default              any                 `yaml:"default"`
		Enum                 []string            `yaml:"enum"`
		Required             []string            `yaml:"required"`
		AdditionalProperties *bool               `yaml:"additionalProperties"`
		Properties           map[string]property `yaml:"properties"`
	}
	type schema struct {
		AdditionalProperties *bool               `yaml:"additionalProperties"`
		Required             []string            `yaml:"required"`
		Properties           map[string]property `yaml:"properties"`
	}
	var contract struct {
		Components struct {
			Schemas map[string]schema `yaml:"schemas"`
		} `yaml:"components"`
	}

	contents, err := os.ReadFile("../../../../contracts/openapi/control-plane-v1.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}
	if err := yaml.Unmarshal(contents, &contract); err != nil {
		t.Fatalf("parse OpenAPI contract: %v", err)
	}

	tests := []struct {
		schema, property string
		wantLimit        int
	}{
		{schema: "PublicEndpoint", property: "hostnameLabel", wantLimit: 63},
		{schema: "RuntimePort", property: "name", wantLimit: 15},
		{schema: "Probe", property: "path", wantLimit: 2048},
		{schema: "Variable", property: "name", wantLimit: 253},
		{schema: "Variable", property: "value", wantLimit: 4096},
	}
	for _, tt := range tests {
		t.Run(tt.schema+"."+tt.property, func(t *testing.T) {
			got := contract.Components.Schemas[tt.schema].Properties[tt.property].MaxLength
			if got == nil || *got != tt.wantLimit {
				t.Fatalf("OpenAPI %s.%s maxLength=%v, domain limit=%d", tt.schema, tt.property, got, tt.wantLimit)
			}
		})
	}

	configuration := contract.Components.Schemas["RuntimeConfiguration"]
	if configuration.AdditionalProperties == nil || *configuration.AdditionalProperties {
		t.Fatal("OpenAPI RuntimeConfiguration must reject unknown fields")
	}
	if !reflect.DeepEqual(configuration.Required, []string{"replicas", "ports", "resources", "probes", "publicEndpoints", "variables", "parameters"}) {
		t.Fatalf("OpenAPI RuntimeConfiguration required fields drifted: %v", configuration.Required)
	}
	assertProperty := func(name string, got property, pattern string, minimum, maximum *int64, defaultValue any, enum []string) {
		t.Helper()
		if got.Pattern != pattern || !reflect.DeepEqual(got.Minimum, minimum) || !reflect.DeepEqual(got.Maximum, maximum) || !reflect.DeepEqual(got.Default, defaultValue) || !reflect.DeepEqual(got.Enum, enum) {
			t.Errorf("OpenAPI %s mismatch: %+v", name, got)
		}
	}
	one, five, cpuMax, memoryMax := int64(1), int64(5), int64(2000), int64(2048)
	assertProperty("RuntimeConfiguration.replicas", configuration.Properties["replicas"], "", &one, &five, 1, nil)
	assertProperty("RuntimePort.name", contract.Components.Schemas["RuntimePort"].Properties["name"], slugPattern.String(), nil, nil, nil, nil)
	assertProperty("PublicEndpoint.hostnameLabel", contract.Components.Schemas["PublicEndpoint"].Properties["hostnameLabel"], slugPattern.String(), nil, nil, nil, nil)
	resources := contract.Components.Schemas["ResourceValues"]
	assertProperty("ResourceValues.cpuMillis", resources.Properties["cpuMillis"], "", &one, &cpuMax, nil, nil)
	assertProperty("ResourceValues.memoryMiB", resources.Properties["memoryMiB"], "", &one, &memoryMax, nil, nil)
	assertProperty("Probe.path", contract.Components.Schemas["Probe"].Properties["path"], "^/", nil, nil, nil, nil)
	for name, value := range map[string]property{"RuntimeConfiguration.resources": configuration.Properties["resources"], "RuntimeConfiguration.probes": configuration.Properties["probes"]} {
		if value.AdditionalProperties == nil || *value.AdditionalProperties {
			t.Errorf("OpenAPI %s must reject unknown fields", name)
		}
	}
	for _, name := range []string{"ResourceValues", "Probe", "RuntimePort", "PublicEndpoint"} {
		value := contract.Components.Schemas[name]
		if value.AdditionalProperties == nil || *value.AdditionalProperties {
			t.Errorf("OpenAPI %s must reject unknown fields", name)
		}
	}
}

func TestValidateRuntimeConfigRejectsAmbiguousParameterBindings(t *testing.T) {
	configuration := ConfigurationFromIntent(validIntent())
	configuration.Parameters = []ParameterBinding{{Name: "DATABASE_URL", ParameterPublicID: "par-abcdefghijklmnopqrst", ParameterVersion: 1}}
	configuration.Variables = []Variable{{Name: "DATABASE_URL", Value: "inline"}}
	if err := ValidateRuntimeConfig(configuration, 5, 2000, 2048); err == nil {
		t.Fatal("ValidateRuntimeConfig accepted duplicate inline and parameter names")
	}
	configuration.Variables = nil
	configuration.Parameters[0].ParameterVersion = 0
	if err := ValidateRuntimeConfig(configuration, 5, 2000, 2048); err == nil {
		t.Fatal("ValidateRuntimeConfig accepted a non-positive parameter version")
	}
}

func TestGeneratedCRDPreservesRuntimeIntentAndIntentionalQuotaAsymmetry(t *testing.T) {
	type validation struct {
		Rule string `yaml:"rule"`
	}
	type property struct {
		Pattern      string              `yaml:"pattern"`
		Minimum      *int64              `yaml:"minimum"`
		Maximum      *int64              `yaml:"maximum"`
		MaxLength    *int                `yaml:"maxLength"`
		Default      any                 `yaml:"default"`
		Enum         []string            `yaml:"enum"`
		Properties   map[string]property `yaml:"properties"`
		Items        *property           `yaml:"items"`
		XValidations []validation        `yaml:"x-kubernetes-validations"`
	}
	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema property `yaml:"openAPIV3Schema"`
				} `yaml:"schema"`
			} `yaml:"versions"`
		} `yaml:"spec"`
	}
	contents, err := os.ReadFile("../../../../deploy/crds/platform.molejo.dev_appdeployments.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(contents, &crd); err != nil {
		t.Fatal(err)
	}
	if len(crd.Spec.Versions) != 1 {
		t.Fatalf("expected one served CRD version, got %d", len(crd.Spec.Versions))
	}
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	one, portMax := int64(1), int64(65535)
	if spec.Properties["image"].Pattern != imagePattern.String() {
		t.Error("CRD image pattern drifted from the domain")
	}
	if !reflect.DeepEqual(spec.Properties["replicas"].Minimum, &one) || spec.Properties["replicas"].Maximum != nil || spec.Properties["replicas"].Default != 1 {
		t.Error("CRD replicas must preserve the runtime minimum/default while product quota remains API-only")
	}
	portItems := spec.Properties["ports"].Items
	if portItems == nil || !reflect.DeepEqual(portItems.Properties["containerPort"].Minimum, &one) || !reflect.DeepEqual(portItems.Properties["containerPort"].Maximum, &portMax) {
		t.Error("CRD port bounds drifted from the domain")
	}
	resources := spec.Properties["resources"]
	for _, side := range []string{"requests", "limits"} {
		for _, resource := range []string{"cpuMillis", "memoryMiB"} {
			value := resources.Properties[side].Properties[resource]
			if !reflect.DeepEqual(value.Minimum, &one) || value.Maximum != nil {
				t.Errorf("CRD %s.%s must enforce positivity without duplicating product quotas", side, resource)
			}
		}
	}
	rules := make([]string, 0, len(spec.XValidations)+len(resources.XValidations))
	for _, item := range append(spec.XValidations, resources.XValidations...) {
		rules = append(rules, item.Rule)
	}
	for _, expected := range []string{"self.requests.cpuMillis <= self.limits.cpuMillis", "self.requests.memoryMiB <= self.limits.memoryMiB"} {
		if !contains(rules, expected) {
			t.Errorf("CRD is missing semantic relation %q", expected)
		}
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestNewPublicID(t *testing.T) {
	id, err := NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "ap-") || len(id) != 23 {
		t.Fatalf("unexpected id %q", id)
	}
}
