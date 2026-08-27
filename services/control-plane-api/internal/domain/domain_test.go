package domain

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func validIntent() Intent {
	return Intent{Name: "demo", Image: "ghcr.io/example/demo@sha256:" + strings.Repeat("a", 64), Replicas: 1, Port: 8080, Resources: Resources{Requests: ResourceValues{CPUMillis: 50, MemoryMiB: 64}, Limits: ResourceValues{CPUMillis: 100, MemoryMiB: 128}}, Probes: Probes{Liveness: Probe{Path: "/healthz"}, Readiness: Probe{Path: "/readyz"}}, Exposure: ExposurePrivate}
}

func TestValidateIntent(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Intent)
		wantErr bool
	}{{"valid", func(i *Intent) {}, false}, {"mutable tag", func(i *Intent) { i.Image = "ghcr.io/example/demo:latest" }, true}, {"request above limit", func(i *Intent) { i.Resources.Requests.CPUMillis = 101 }, true}, {"private slug", func(i *Intent) { i.Slug = "demo" }, true}, {"public slug", func(i *Intent) { i.Exposure = ExposurePublic; i.Slug = "demo" }, false}, {"invalid probe", func(i *Intent) { i.Probes.Readiness.Path = "ready" }, true}}
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

func TestNormalizeIntentAppliesOnlyPublicDefaults(t *testing.T) {
	tests := []struct {
		name         string
		intent       Intent
		wantReplicas int32
		wantExposure string
	}{
		{name: "defaults", intent: Intent{}, wantReplicas: 1, wantExposure: ExposurePrivate},
		{name: "explicit values", intent: Intent{Replicas: 3, Exposure: ExposurePublic}, wantReplicas: 3, wantExposure: ExposurePublic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeIntent(tt.intent)
			if got.Replicas != tt.wantReplicas || got.Exposure != tt.wantExposure {
				t.Fatalf("NormalizeIntent() replicas=%d exposure=%q, want replicas=%d exposure=%q", got.Replicas, got.Exposure, tt.wantReplicas, tt.wantExposure)
			}
		})
	}
}

func TestValidateIntentDoesNotSilentlyApplyDefaults(t *testing.T) {
	intent := validIntent()
	intent.Replicas = 0
	intent.Exposure = ""
	if err := ValidateIntent(intent, 5, 2000, 2048); err == nil {
		t.Fatal("ValidateIntent accepted omitted values instead of requiring explicit normalization")
	}
}

func TestOpenAPIIntentConstraintsMatchDomainBoundaries(t *testing.T) {
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
	type alternative struct {
		Required   []string            `yaml:"required"`
		Properties map[string]property `yaml:"properties"`
		Not        struct {
			Required []string `yaml:"required"`
		} `yaml:"not"`
	}
	type schema struct {
		AdditionalProperties *bool               `yaml:"additionalProperties"`
		Required             []string            `yaml:"required"`
		Properties           map[string]property `yaml:"properties"`
		AllOf                []struct {
			OneOf []alternative `yaml:"oneOf"`
		} `yaml:"allOf"`
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
		schema    string
		property  string
		wantLimit int
	}{
		{schema: "DeploymentIntent", property: "name", wantLimit: 63},
		{schema: "DeploymentIntent", property: "slug", wantLimit: 63},
		{schema: "Probe", property: "path", wantLimit: 2048},
	}
	for _, tt := range tests {
		t.Run(tt.schema+"."+tt.property, func(t *testing.T) {
			got := contract.Components.Schemas[tt.schema].Properties[tt.property].MaxLength
			if got == nil || *got != tt.wantLimit {
				t.Fatalf("OpenAPI %s.%s maxLength=%v, domain limit=%d", tt.schema, tt.property, got, tt.wantLimit)
			}
		})
	}

	intent := contract.Components.Schemas["DeploymentIntent"]
	if intent.AdditionalProperties == nil || *intent.AdditionalProperties {
		t.Fatal("OpenAPI DeploymentIntent must reject unknown fields")
	}
	if !reflect.DeepEqual(intent.Required, []string{"name", "image", "port", "resources", "probes"}) {
		t.Fatalf("OpenAPI DeploymentIntent required fields drifted: %v", intent.Required)
	}
	assertProperty := func(name string, got property, pattern string, minimum, maximum *int64, defaultValue any, enum []string) {
		t.Helper()
		if got.Pattern != pattern || !reflect.DeepEqual(got.Minimum, minimum) || !reflect.DeepEqual(got.Maximum, maximum) || !reflect.DeepEqual(got.Default, defaultValue) || !reflect.DeepEqual(got.Enum, enum) {
			t.Errorf("OpenAPI %s mismatch: %+v", name, got)
		}
	}
	one, five, portMax, cpuMax, memoryMax := int64(1), int64(5), int64(65535), int64(2000), int64(2048)
	assertProperty("DeploymentIntent.name", intent.Properties["name"], namePattern.String(), nil, nil, nil, nil)
	assertProperty("DeploymentIntent.image", intent.Properties["image"], imagePattern.String(), nil, nil, nil, nil)
	assertProperty("DeploymentIntent.replicas", intent.Properties["replicas"], "", &one, &five, 1, nil)
	assertProperty("DeploymentIntent.port", intent.Properties["port"], "", &one, &portMax, nil, nil)
	assertProperty("DeploymentIntent.exposure", intent.Properties["exposure"], "", nil, nil, ExposurePrivate, []string{ExposurePrivate, ExposurePublic})
	assertProperty("DeploymentIntent.slug", intent.Properties["slug"], namePattern.String(), nil, nil, nil, nil)
	resources := contract.Components.Schemas["ResourceValues"]
	assertProperty("ResourceValues.cpuMillis", resources.Properties["cpuMillis"], "", &one, &cpuMax, nil, nil)
	assertProperty("ResourceValues.memoryMiB", resources.Properties["memoryMiB"], "", &one, &memoryMax, nil, nil)
	assertProperty("Probe.path", contract.Components.Schemas["Probe"].Properties["path"], "^/", nil, nil, nil, nil)
	for name, value := range map[string]property{"DeploymentIntent.resources": intent.Properties["resources"], "DeploymentIntent.probes": intent.Properties["probes"]} {
		if value.AdditionalProperties == nil || *value.AdditionalProperties {
			t.Errorf("OpenAPI %s must reject unknown fields", name)
		}
	}
	for _, name := range []string{"ResourceValues", "Probe"} {
		value := contract.Components.Schemas[name]
		if value.AdditionalProperties == nil || *value.AdditionalProperties {
			t.Errorf("OpenAPI %s must reject unknown fields", name)
		}
	}
	if len(intent.AllOf) != 1 || len(intent.AllOf[0].OneOf) != 2 || !reflect.DeepEqual(intent.AllOf[0].OneOf[0].Required, []string{"exposure", "slug"}) || !reflect.DeepEqual(intent.AllOf[0].OneOf[0].Properties["exposure"].Enum, []string{ExposurePublic}) || !reflect.DeepEqual(intent.AllOf[0].OneOf[1].Properties["exposure"].Enum, []string{ExposurePrivate}) || !reflect.DeepEqual(intent.AllOf[0].OneOf[1].Not.Required, []string{"slug"}) {
		t.Fatalf("OpenAPI public/private slug relation drifted: %+v", intent.AllOf)
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
	contents, err := os.ReadFile("../../../../deploy/crds/platform.fruto.calouro.tech_appdeployments.yaml")
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
	if !reflect.DeepEqual(spec.Properties["port"].Minimum, &one) || !reflect.DeepEqual(spec.Properties["port"].Maximum, &portMax) {
		t.Error("CRD port bounds drifted from the domain")
	}
	if !reflect.DeepEqual(spec.Properties["exposure"].Enum, []string{ExposurePrivate, ExposurePublic}) || spec.Properties["exposure"].Default != ExposurePrivate {
		t.Error("CRD exposure contract drifted from the domain")
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
	for _, expected := range []string{"self.exposure != 'Public' || has(self.slug)", "self.exposure != 'Private' || !has(self.slug)", "self.requests.cpuMillis <= self.limits.cpuMillis", "self.requests.memoryMiB <= self.limits.memoryMiB"} {
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
