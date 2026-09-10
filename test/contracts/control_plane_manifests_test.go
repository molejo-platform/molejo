package contracts

import (
	"os"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

func TestControlPlanePostgresUsesPortablePersistentStorage(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/control-plane/postgres.yaml")
	if err != nil {
		t.Fatal(err)
	}
	documents := strings.Split(string(contents), "---")
	var statefulSet appsv1.StatefulSet
	for _, document := range documents {
		var metadata struct {
			Kind string `yaml:"kind"`
		}
		_ = yaml.Unmarshal([]byte(document), &metadata)
		if metadata.Kind == "StatefulSet" {
			if err = yaml.Unmarshal([]byte(document), &statefulSet); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(statefulSet.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("volume claims=%d", len(statefulSet.Spec.VolumeClaimTemplates))
	}
	claim := statefulSet.Spec.VolumeClaimTemplates[0]
	if claim.Spec.StorageClassName != nil || len(claim.Spec.AccessModes) != 1 || claim.Spec.Resources.Requests.Storage().Cmp(resource.MustParse("2Gi")) != 0 {
		t.Fatalf("PVC is not portable: %+v", claim.Spec)
	}
	container := statefulSet.Spec.Template.Spec.Containers[0]
	if !strings.Contains(container.Image, "postgres:17.6-bookworm@sha256:") {
		t.Fatalf("PostgreSQL image is not pinned: %s", container.Image)
	}
	if container.Resources.Requests.Cpu().String() != "100m" || container.Resources.Requests.Memory().String() != "128Mi" || container.Resources.Limits.Memory().String() != "256Mi" {
		t.Fatalf("PostgreSQL resources=%+v", container.Resources)
	}
	if container.ReadinessProbe == nil || container.LivenessProbe == nil {
		t.Fatal("PostgreSQL probes are incomplete")
	}
}

func TestControlPlaneDeploymentsSeparateAPIAndConsole(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/control-plane/deployments.yaml")
	if err != nil {
		t.Fatal(err)
	}
	deployments := map[string]appsv1.Deployment{}
	for _, document := range strings.Split(string(contents), "---") {
		var deployment appsv1.Deployment
		if err = yaml.Unmarshal([]byte(document), &deployment); err != nil {
			t.Fatal(err)
		}
		if deployment.Kind == "Deployment" {
			deployments[deployment.Name] = deployment
		}
	}
	apiDeployment, apiFound := deployments["control-plane-api"]
	consoleDeployment, consoleFound := deployments["console-web"]
	if !apiFound || !consoleFound || len(apiDeployment.Spec.Template.Spec.Containers) != 1 || len(consoleDeployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("deployments=%+v", deployments)
	}
	if apiDeployment.Spec.Template.Spec.Containers[0].Name != "api" || consoleDeployment.Spec.Template.Spec.Containers[0].Name != "console" {
		t.Fatalf("API and console boundaries are not explicit")
	}
	text := string(contents)
	for _, forbidden := range []string{"OpenBao", "ClickHouse", "GitHub", "HTTPRoute"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("control plane deployment retains %q", forbidden)
		}
	}
}
