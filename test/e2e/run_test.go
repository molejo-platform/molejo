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
	rolloutStart := strings.Index(script, "kubectl --kubeconfig \"${KUBECONFIG_FILE}\" rollout status")
	if loadEnd < 0 || rolloutStart <= loadEnd {
		t.Fatal("expected the image load to precede the operator rollout")
	}
	installBody := script[loadEnd+len(loadImage) : rolloutStart]
	if !strings.Contains(installBody, `${OPERATOR_IMAGE}`) {
		t.Error("expected the per-run operator image to be projected into the installed Deployment")
	}
}
