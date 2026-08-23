package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestK3SFrontendValidationUsesAnExplicitSafeContext(t *testing.T) {
	contents, err := os.ReadFile("k3s-frontend.sh")
	if err != nil {
		t.Fatalf("read k3s frontend script: %v", err)
	}
	script := string(contents)

	checks := map[string]string{
		"defaults to the dedicated context":   `FRUTO_KUBE_CONTEXT:-fruto-lab`,
		"passes the context to kubectl":       `kubectl --context "${KUBE_CONTEXT}"`,
		"rejects accidental custom contexts":  "refusing non-fruto context",
		"does not change the current context": "kube() {",
	}
	for description, expected := range checks {
		if !strings.Contains(script, expected) {
			t.Errorf("expected k3s validation to %s", description)
		}
	}
	if strings.Contains(script, "kubectl config use-context") {
		t.Error("k3s validation must not change the user's current Kubernetes context")
	}
	if strings.Contains(script, "--insecure") || strings.Contains(script, "-k ") {
		t.Error("k3s validation must not bypass TLS verification")
	}
}

func TestK3SFrontendValidationKeepsPublicDigestPinnedWorkloads(t *testing.T) {
	contents, err := os.ReadFile("k3s-frontend.sh")
	if err != nil {
		t.Fatalf("read k3s frontend script: %v", err)
	}
	script := string(contents)

	checks := map[string]string{
		"publishes only amd64 images":           "--platform linux/amd64",
		"addresses static HTML by digest":       `STATIC_IMAGE="${STATIC_REPOSITORY}@${static_digest}"`,
		"addresses the SPA by digest":           `SPA_IMAGE_V2="${SPA_REPOSITORY}@${spa_v2_digest}"`,
		"copies the registry pull credential":   `REGISTRY_SECRET_NAME`,
		"validates the static public URL":       "https://${STATIC_HOSTNAME}/",
		"validates the SPA deep link":           "https://${SPA_HOSTNAME}/projects/example",
		"validates the second SPA release":      `content="v2"`,
		"preserves identities during rollout":   "k3s rollout replaced a logical Kubernetes child",
		"leaves stable validation applications": "phase 4 k3s validation passed",
	}
	for description, expected := range checks {
		if !strings.Contains(script, expected) {
			t.Errorf("expected k3s validation to %s", description)
		}
	}
	if strings.Contains(script, "delete namespace") || strings.Contains(script, "delete appdeployment") {
		t.Error("k3s validation must keep the public phase 4 applications available")
	}
}

func TestK3SFrontendValidationProvesServiceDriftWasRemoved(t *testing.T) {
	contents, err := os.ReadFile("k3s-frontend.sh")
	if err != nil {
		t.Fatalf("read k3s frontend script: %v", err)
	}
	script := string(contents)

	start := strings.Index(script, `patch "service/${STATIC_APP}"`)
	end := strings.Index(script, `echo "phase 4 k3s validation passed"`)
	if start < 0 || end <= start {
		t.Fatal("expected k3s Service drift validation section")
	}
	if !strings.Contains(script[start:end], `.spec.selector.drift`) {
		t.Error("expected k3s validation to assert that the injected selector drift was removed")
	}
}

func TestK3SFrontendValidationWaitsForCurrentPublicGeneration(t *testing.T) {
	contents, err := os.ReadFile("k3s-frontend.sh")
	if err != nil {
		t.Fatalf("read k3s frontend script: %v", err)
	}
	script := string(contents)

	start := strings.Index(script, `"exposure":"Public","slug":"static"`)
	end := strings.Index(script, `kube apply -f "${SPA_MANIFEST}"`)
	if start < 0 || end <= start {
		t.Fatal("expected static frontend public promotion section")
	}
	promotion := script[start:end]
	if !strings.Contains(promotion, `.status.observedGeneration`) {
		t.Error("expected k3s validation to wait until the public generation was observed")
	}
	if !strings.Contains(promotion, `--for=condition=Ready`) {
		t.Error("expected k3s validation to validate Ready after public promotion")
	}
}

func TestK3SRedirectRetriesHavePerRequestTimeouts(t *testing.T) {
	contents, err := os.ReadFile("k3s-frontend.sh")
	if err != nil {
		t.Fatalf("read k3s frontend script: %v", err)
	}
	script := string(contents)

	start := strings.Index(script, `wait_for_redirect() {`)
	end := strings.Index(script, `wait_for_redirect "http://${STATIC_HOSTNAME}/"`)
	if start < 0 || end <= start {
		t.Fatal("expected wait_for_redirect function")
	}
	redirect := script[start:end]
	for _, option := range []string{"--connect-timeout", "--max-time"} {
		if !strings.Contains(redirect, option) {
			t.Errorf("expected each redirect attempt to use %s", option)
		}
	}
}
