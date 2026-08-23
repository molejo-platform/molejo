package domain

import (
	"strings"
	"testing"
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

func TestNewPublicID(t *testing.T) {
	id, err := NewPublicID("dep")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "dep-") || len(id) != 24 {
		t.Fatalf("unexpected id %q", id)
	}
}
