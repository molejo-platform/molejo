package release

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	valid := RegisterCommand{
		Artifact:   Artifact{Kind: ArtifactOCIImage, Reference: "registry.example/app@sha256:" + strings.Repeat("a", 64)},
		Source:     Source{Provider: "GitHub", Repository: "molejo-platform/molejo", Revision: strings.Repeat("b", 40), Ref: "refs/heads/main"},
		Provenance: Provenance{Producer: "github-actions", ExternalRunID: "123", URL: "https://github.com/molejo-platform/molejo/actions/runs/123"},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*RegisterCommand)
	}{
		{name: "tagged artifact", mutate: func(value *RegisterCommand) { value.Artifact.Reference = "registry.example/app:latest" }},
		{name: "unknown kind", mutate: func(value *RegisterCommand) { value.Artifact.Kind = "Tarball" }},
		{name: "missing source", mutate: func(value *RegisterCommand) { value.Source.Repository = "" }},
		{name: "relative provenance URL", mutate: func(value *RegisterCommand) { value.Provenance.URL = "/runs/123" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := Validate(value); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}
