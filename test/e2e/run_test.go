package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestE2EOperatorImageIsIsolatedAndRemoved(t *testing.T) {
	contents, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatalf("read E2E script: %v", err)
	}
	script := string(contents)

	var operatorImageDeclaration string
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "readonly OPERATOR_IMAGE=") {
			operatorImageDeclaration = line
			break
		}
	}
	if operatorImageDeclaration == "" {
		t.Fatal("expected E2E script to declare OPERATOR_IMAGE")
	}
	if !strings.Contains(operatorImageDeclaration, "$$") {
		t.Errorf("expected operator image tag to be isolated per run, got %q", operatorImageDeclaration)
	}

	finishStart := strings.Index(script, "finish() {")
	finishEnd := strings.Index(script, "trap finish EXIT")
	if finishStart < 0 || finishEnd <= finishStart {
		t.Fatal("expected E2E script to define the finish trap")
	}
	finishBody := script[finishStart:finishEnd]
	if !strings.Contains(finishBody, `"${OPERATOR_IMAGE}"`) {
		t.Error("expected E2E cleanup to remove the per-run operator image")
	}

	loadImage := `kind_cli load docker-image --name "${CLUSTER_NAME}" "${OPERATOR_IMAGE}"`
	loadEnd := strings.Index(script, loadImage)
	if loadEnd < 0 {
		t.Fatal("expected the operator image to be loaded into Kind")
	}
	rolloutOffset := strings.Index(script[loadEnd:], "kubectl --kubeconfig \"${KUBECONFIG_FILE}\" rollout status")
	if rolloutOffset < 0 {
		t.Fatal("expected the image load to precede the operator rollout")
	}
	rolloutStart := loadEnd + rolloutOffset
	installBody := script[loadEnd+len(loadImage) : rolloutStart]
	if !strings.Contains(installBody, `${OPERATOR_IMAGE}`) {
		t.Error("expected the per-run operator image to be projected into the installed Deployment")
	}
}

func TestE2ECoversPersistentPublicTransports(t *testing.T) {
	contents, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatalf("read E2E script: %v", err)
	}
	script := string(contents)

	checks := map[string]string{
		"declares a bounded persistence duration":     "readonly PERSISTENT_TRANSPORT_SECONDS=11",
		"keeps the SSE request in the background":     `PUBLIC_SSE_PID=$!`,
		"fails when SSE closes before the duration":   `kill -0 "${PUBLIC_SSE_PID}"`,
		"requires incremental SSE delivery":           `final_sse_event_count <= initial_sse_event_count`,
		"keeps WebSocket idle before another message": `--idle-duration "${PERSISTENT_TRANSPORT_SECONDS}s"`,
	}
	for description, expected := range checks {
		if !strings.Contains(script, expected) {
			t.Errorf("expected E2E to %s", description)
		}
	}
}
