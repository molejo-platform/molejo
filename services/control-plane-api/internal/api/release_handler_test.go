package api

import (
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/release"
)

func TestReleaseRegistrationCommandMapsTheGeneratedContract(t *testing.T) {
	ref := "refs/heads/main"
	externalRunID := "run-42"
	url := "https://ci.example/runs/42"
	var input generated.ReleaseRegistrationInput
	input.Artifact.Kind = generated.OCIImage
	input.Artifact.Reference = "registry.example/example/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	input.Source.Provider = "example-source"
	input.Source.Repository = "example/app"
	input.Source.Revision = "revision-42"
	input.Source.Ref = &ref
	input.Provenance.Producer = "example-ci"
	input.Provenance.ExternalRunId = &externalRunID
	input.Provenance.Url = &url

	command := releaseRegistrationCommand(input)

	if command.Artifact.Kind != release.ArtifactOCIImage || command.Artifact.Reference != input.Artifact.Reference {
		t.Fatalf("artifact mapping = %+v", command.Artifact)
	}
	if command.Source.Provider != input.Source.Provider || command.Source.Repository != input.Source.Repository || command.Source.Revision != input.Source.Revision || command.Source.Ref != ref {
		t.Fatalf("source mapping = %+v", command.Source)
	}
	if command.Provenance.Producer != input.Provenance.Producer || command.Provenance.ExternalRunID != externalRunID || command.Provenance.URL != url {
		t.Fatalf("provenance mapping = %+v", command.Provenance)
	}
}

func TestReleaseRegistrationCommandMapsAbsentOptionalValuesToEmptyStrings(t *testing.T) {
	var input generated.ReleaseRegistrationInput

	command := releaseRegistrationCommand(input)

	if command.Source.Ref != "" || command.Provenance.ExternalRunID != "" || command.Provenance.URL != "" {
		t.Fatalf("optional values mapping = source=%+v provenance=%+v", command.Source, command.Provenance)
	}
}
