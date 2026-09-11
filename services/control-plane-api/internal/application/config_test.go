package application

import "testing"

func TestLoadHTTPConfigValidatesEnvironmentValues(t *testing.T) {
	t.Setenv("MOLEJO_SESSION_TTL", "not-a-duration")
	if _, err := loadHTTPConfig(); err == nil {
		t.Fatal("invalid session duration was accepted")
	}

	t.Setenv("MOLEJO_SESSION_TTL", "3h")
	t.Setenv("MOLEJO_PUBLIC_URL", "https://control.molejo.dev")
	t.Setenv("MOLEJO_ALLOWED_ORIGIN", "https://control.molejo.dev")
	t.Setenv("MOLEJO_COOKIE_SECURE", "true")
	t.Setenv("MOLEJO_ALLOWED_HOSTS", "control.molejo.dev, localhost:8080")
	t.Setenv("MOLEJO_ALLOWED_REGISTRIES", "ghcr.io, registry.molejo.dev")
	configuration, err := loadHTTPConfig()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.SessionTTL.String() != "3h0m0s" || len(configuration.AllowedHosts) != 2 || len(configuration.AllowedRegistries) != 2 || configuration.AllowedRegistries[1] != "registry.molejo.dev" {
		t.Fatalf("configuration = %+v", configuration)
	}
}
