package contracts

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const testObservabilitySecret = "molejo-observability-credentials-aaaaaaaaaaaa"

func TestObservabilityApplyIsIdempotentWithoutImperativeRestarts(t *testing.T) {
	root := repositoryRoot(t)
	temporary := t.TempDir()
	logPath := filepath.Join(temporary, "kubectl.log")
	installFakeKubectl(t, temporary)
	prepareFakeObservabilityRelease(t, temporary)
	scriptPath := filepath.Join(root, "test/e2e/apply-observability-k3s.sh")

	runObservabilityApply(t, root, temporary, logPath, scriptPath)
	firstRestartCount := restartCommandCount(t, logPath)
	runObservabilityApply(t, root, temporary, logPath, scriptPath)
	secondRestartCount := restartCommandCount(t, logPath)

	if firstRestartCount != 0 || secondRestartCount != 0 {
		t.Fatalf("observability apply issued imperative restarts: first=%d second=%d", firstRestartCount, secondRestartCount)
	}
}

func TestObservabilityConfigurationChangeRollsOnlyItsConsumer(t *testing.T) {
	root := repositoryRoot(t)
	temporary := t.TempDir()
	deployRoot := filepath.Join(temporary, "deploy")
	if err := os.CopyFS(filepath.Join(deployRoot, "observability"), os.DirFS(filepath.Join(root, "deploy/observability"))); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(filepath.Join(deployRoot, "observability-lab"), os.DirFS(filepath.Join(root, "deploy/observability-lab"))); err != nil {
		t.Fatal(err)
	}

	before := renderObservability(t, filepath.Join(deployRoot, "observability-lab"))
	gatewayConfig := filepath.Join(deployRoot, "observability/config/otel-gateway.yaml")
	file, err := os.OpenFile(gatewayConfig, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("\n# rollout-contract-change\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	after := renderObservability(t, filepath.Join(deployRoot, "observability-lab"))

	for _, workload := range []struct {
		kind, name, volume string
		changed            bool
	}{
		{"Deployment", "otel-gateway", "config", true},
		{"Deployment", "otel-cluster", "config", false},
		{"DaemonSet", "otel-agent", "config", false},
		{"StatefulSet", "clickhouse", "config", false},
	} {
		beforeRef := renderedConfigMapRef(t, before, workload.kind, workload.name, workload.volume)
		afterRef := renderedConfigMapRef(t, after, workload.kind, workload.name, workload.volume)
		if (beforeRef != afterRef) != workload.changed {
			t.Fatalf("%s/%s config reference before=%q after=%q changed=%v", workload.kind, workload.name, beforeRef, afterRef, workload.changed)
		}
	}
}

func renderObservability(t *testing.T, path string) []*unstructured.Unstructured {
	t.Helper()
	command := exec.Command("kubectl", "--context", "fruto-lab", "kustomize", path)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("render observability manifests: %v", err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	var objects []*unstructured.Unstructured
	for {
		object := &unstructured.Unstructured{}
		if err = decoder.Decode(object); err == io.EOF {
			return objects
		}
		if err != nil {
			t.Fatal(err)
		}
		if object.GetKind() != "" {
			objects = append(objects, object)
		}
	}
}

func renderedConfigMapRef(t *testing.T, objects []*unstructured.Unstructured, kind, name, volumeName string) string {
	t.Helper()
	for _, object := range objects {
		if object.GetKind() != kind || object.GetName() != name {
			continue
		}
		volumes, found, err := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "volumes")
		if err != nil || !found {
			t.Fatalf("%s/%s volumes missing", kind, name)
		}
		for _, item := range volumes {
			volume := item.(map[string]any)
			if volume["name"] != volumeName {
				continue
			}
			ref, found, err := unstructured.NestedString(volume, "configMap", "name")
			if err != nil || !found {
				t.Fatalf("%s/%s volume %s has no ConfigMap", kind, name, volumeName)
			}
			return ref
		}
	}
	t.Fatalf("%s/%s volume %s not found", kind, name, volumeName)
	return ""
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(workingDirectory, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func installFakeKubectl(t *testing.T, directory string) {
	t.Helper()
	fake := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_KUBECTL_LOG"
if [[ "$*" == *"get namespace kube-system"* ]]; then
  printf '%s' "$FAKE_CLUSTER_UID"
fi
`
	if err := os.WriteFile(filepath.Join(directory, "kubectl"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
}

func prepareFakeObservabilityRelease(t *testing.T, directory string) {
	t.Helper()
	metadataDir := filepath.Join(directory, "metadata")
	if err := os.MkdirAll(metadataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := "MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET=" + testObservabilitySecret + "\n"
	if err := os.WriteFile(filepath.Join(metadataDir, "observability.env"), []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	release := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: " + testObservabilitySecret + "\n"
	if err := os.WriteFile(filepath.Join(directory, "observability.yaml"), []byte(release), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runObservabilityApply(t *testing.T, root, fakeBin, logPath, scriptPath string) {
	t.Helper()
	command := exec.Command("bash", scriptPath)
	command.Dir = root
	command.Env = append(
		os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_KUBECTL_LOG="+logPath,
		"FAKE_CLUSTER_UID=test-cluster-uid",
		"FRUTO_EXPECTED_CLUSTER_UID=test-cluster-uid",
		"FRUTO_K3S_CONTEXT=fruto-lab",
		"FRUTO_RELEASE_DIR="+fakeBin,
		"FRUTO_OBSERVABILITY_RELEASE_OUTPUT="+filepath.Join(fakeBin, "observability.yaml"),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("observability apply failed with fake kubectl: %v\n%s", err, output)
	}
}

func restartCommandCount(t *testing.T, logPath string) int {
	t.Helper()
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(log), "rollout restart ")
}
