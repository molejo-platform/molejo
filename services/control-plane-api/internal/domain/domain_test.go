package domain

import (
	"os"
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

func TestOpenAPIIntentConstraintsMatchDomainBoundaries(t *testing.T) {
	type property struct {
		MaxLength *int `yaml:"maxLength"`
	}
	type schema struct {
		Properties map[string]property `yaml:"properties"`
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
}

func TestNewPublicID(t *testing.T) {
	id, err := NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "dep-") || len(id) != 24 {
		t.Fatalf("unexpected id %q", id)
	}
}
