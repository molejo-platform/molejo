package api

import "testing"

func TestConfigRejectsInsecurePublicOrigins(t *testing.T) {
	config := DefaultConfig()
	config.Mode = "development"
	config.PublicURL = "http://cloud.molejo.dev"
	config.AllowedOrigin = config.PublicURL
	config.AllowedHosts = []string{"cloud.molejo.dev"}
	config.AllowedRegistries = []string{"ghcr.io"}

	if err := config.Validate(); err == nil {
		t.Fatal("insecure non-local public URL was accepted")
	}
}

func TestConfigAllowsExplicitLocalHTTPAndRequiresSecureProduction(t *testing.T) {
	local := DefaultConfig()
	local.Mode = "development"
	local.PublicURL = "http://console.localhost:5173"
	local.AllowedOrigin = local.PublicURL
	local.AllowedHosts = []string{"console.localhost:5173"}
	local.AllowedRegistries = []string{"ghcr.io"}
	if err := local.Validate(); err != nil {
		t.Fatalf("explicit local development config was rejected: %v", err)
	}

	production := local
	production.Mode = "production"
	production.PublicURL = "https://cloud.molejo.dev"
	production.AllowedOrigin = production.PublicURL
	production.AllowedHosts = []string{"cloud.molejo.dev"}
	if err := production.Validate(); err == nil {
		t.Fatal("production config without Secure cookies was accepted")
	}
	production.CookieSecure = true
	production.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	if err := production.Validate(); err != nil {
		t.Fatalf("secure production config was rejected: %v", err)
	}
}

func TestRegistryAllowlistMatchesTheRegistryHost(t *testing.T) {
	config := DefaultConfig()
	config.AllowedRegistries = []string{"ghcr.io", "registry.molejo.dev:5000"}
	for _, image := range []string{
		"ghcr.io/fruto-platform/testkit@sha256:" + repeatHex('a'),
		"registry.molejo.dev:5000/lab/testkit@sha256:" + repeatHex('b'),
	} {
		if !config.RegistryAllowed(image) {
			t.Fatalf("allowed image %q was rejected", image)
		}
	}
	if config.RegistryAllowed("docker.io/library/nginx@sha256:" + repeatHex('c')) {
		t.Fatal("unconfigured registry was accepted")
	}
}

func repeatHex(value byte) string {
	result := make([]byte, 64)
	for index := range result {
		result[index] = value
	}
	return string(result)
}
