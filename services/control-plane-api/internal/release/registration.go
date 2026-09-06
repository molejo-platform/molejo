// Package release defines provider-independent commands for registering
// immutable application artifacts.
package release

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

const (
	ArtifactOCIImage = "OCIImage"
)

type Artifact struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
}

type Source struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
	Ref        string `json:"ref,omitempty"`
}

type Provenance struct {
	Producer      string `json:"producer"`
	ExternalRunID string `json:"externalRunId,omitempty"`
	URL           string `json:"url,omitempty"`
}

type RegisterCommand struct {
	Artifact   Artifact   `json:"artifact"`
	Source     Source     `json:"source"`
	Provenance Provenance `json:"provenance"`
}

func Validate(command RegisterCommand) error {
	if command.Artifact.Kind != ArtifactOCIImage {
		return errors.New("artifact kind must be OCIImage")
	}
	repository, digest, ok := strings.Cut(command.Artifact.Reference, "@")
	if !ok {
		return errors.New("artifact reference must contain an immutable digest")
	}
	canonical, err := domain.ReleaseImageReference(repository, digest)
	if err != nil || canonical != command.Artifact.Reference {
		return errors.New("artifact reference must be a canonical OCI image digest")
	}
	for _, field := range []struct{ name, value string }{
		{"source provider", command.Source.Provider},
		{"source repository", command.Source.Repository},
		{"source revision", command.Source.Revision},
		{"producer", command.Provenance.Producer},
	} {
		if err = validateText(field.name, field.value, 255, true); err != nil {
			return err
		}
	}
	if err = validateText("source ref", command.Source.Ref, 255, false); err != nil {
		return err
	}
	if err = validateText("external run ID", command.Provenance.ExternalRunID, 255, false); err != nil {
		return err
	}
	if command.Provenance.URL != "" {
		if len(command.Provenance.URL) > 2048 {
			return errors.New("provenance URL is invalid")
		}
		parsed, parseErr := url.ParseRequestURI(command.Provenance.URL)
		if parseErr != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return errors.New("provenance URL must be an absolute HTTP(S) URL")
		}
	}
	return nil
}

func validateText(name, value string, maximum int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return errors.New(name + " is required")
	}
	if value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > maximum || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return errors.New(name + " is invalid")
	}
	return nil
}
