package registrysetup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFilterDockerConfigKeepsOnlyTargetRegistry(t *testing.T) {
	source := []byte(`{"auths":{"registry.molejo.dev":{"auth":"dXNlcjpwYXNz"},"ghcr.io":{"auth":"b3RoZXI6cGFzcw=="}}}`)
	filtered, err := FilterDockerConfig(source, "registry.molejo.dev")
	if err != nil {
		t.Fatal(err)
	}
	var config dockerConfig
	if err = json.Unmarshal(filtered, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Auths) != 1 || config.Auths["registry.molejo.dev"] == nil || strings.Contains(string(filtered), "ghcr.io") {
		t.Fatalf("filtered=%s", filtered)
	}
}

func TestFilterDockerConfigAcceptsCanonicalDockerKey(t *testing.T) {
	source := []byte(`{"auths":{"https://registry.molejo.dev/v1/":{"username":"user","password":"pass"}}}`)
	filtered, err := FilterDockerConfig(source, "registry.molejo.dev")
	if err != nil || !DockerConfigContains(filtered, "registry.molejo.dev") {
		t.Fatalf("filtered=%s err=%v", filtered, err)
	}
}

func TestFilterDockerConfigRejectsMissingOrExternalCredentials(t *testing.T) {
	for _, source := range []string{
		`{"auths":{"ghcr.io":{"auth":"b3RoZXI6cGFzcw=="}}}`,
		`{"auths":{"registry.molejo.dev":{}}}`,
		`not-json`,
	} {
		if _, err := FilterDockerConfig([]byte(source), "registry.molejo.dev"); err == nil {
			t.Fatalf("source unexpectedly accepted: %s", source)
		}
	}
}
