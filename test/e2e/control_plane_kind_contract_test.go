package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestControlPlaneKindUsesTheCurrentHierarchyContract(t *testing.T) {
	contents, err := os.ReadFile("control-plane-kind.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(contents)
	if strings.Contains(script, "/api/v1/deployments") {
		t.Fatal("control-plane Kind E2E still uses the retired top-level Deployment API")
	}
	for _, expected := range []string{
		"/workspaces/current",
		"/projects",
		"/environments",
		"/apps",
		"seed_test_release",
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("control-plane Kind E2E does not exercise %q", expected)
		}
	}
	if strings.Contains(script, "Phase 6") || strings.Contains(script, "phase6-") {
		t.Fatal("control-plane Kind E2E still identifies itself as a retired roadmap phase")
	}
}

func TestLocalControlPlaneDisablesExternalSecretBackend(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/control-plane-local/configmap.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `FRUTO_OPENBAO_ADDR: ""`) {
		t.Fatal("local control plane must explicitly disable OpenBao instead of depending on an external service")
	}
}

func TestLocalControlPlaneOmitsTheOutOfBandBootstrapJob(t *testing.T) {
	kustomization, err := os.ReadFile("../../deploy/control-plane-local/kustomization.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kustomization), "delete-bootstrap-job.yaml") {
		t.Fatal("local overlay must omit the bootstrap Job because the E2E creates its disposable owner out of band")
	}
	patch, err := os.ReadFile("../../deploy/control-plane-local/delete-bootstrap-job.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patch), "$patch: delete") {
		t.Fatal("local bootstrap Job patch must delete the resource")
	}
}

func TestE2EFixturesDoNotEncodeRetiredRoadmapPhases(t *testing.T) {
	for _, path := range []string{
		"run.sh",
		"k3s-frontend.sh",
		"appdeployment.yaml",
		"spa-appdeployment.yaml",
		"../fixtures/http-app/main.go",
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(contents))
		for _, retired := range []string{"phase2", "phase 2", "phase3", "phase 3", "phase4", "phase 4"} {
			if strings.Contains(lower, retired) {
				t.Errorf("%s still encodes retired roadmap label %q", path, retired)
			}
		}
	}
}

func TestFrontendFixturesDoNotFetchARemoteDockerfileFrontend(t *testing.T) {
	for _, path := range []string{
		"../fixtures/static-html/Dockerfile",
		"../fixtures/vite-react-spa/Dockerfile",
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), "# syntax=") {
			t.Errorf("%s makes the deterministic E2E depend on a remote Dockerfile frontend", path)
		}
	}
}
