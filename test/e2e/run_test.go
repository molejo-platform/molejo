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

func TestControlPlaneE2ERestartPinsTheCrashAfterOperationClaim(t *testing.T) {
	contents, err := os.ReadFile("control-plane-kind.sh")
	if err != nil {
		t.Fatalf("read control-plane E2E script: %v", err)
	}
	script := string(contents)

	restartStart := strings.Index(script, "Idempotency-Key: phase6-restart")
	restartEnd := strings.Index(script, "unknown_response=")
	if restartStart < 0 || restartEnd <= restartStart {
		t.Fatal("expected the control-plane restart scenario")
	}
	restart := script[restartStart:restartEnd]
	if !strings.Contains(restart, `[[ "$pending_status" == Running ]]`) {
		t.Fatal("restart can happen while the operation is either Pending or Running; the crash window is not pinned after ClaimNext")
	}
}

func TestControlPlaneE2EIsolatesHostResourcesAndSecrets(t *testing.T) {
	contents, err := os.ReadFile("control-plane-kind.sh")
	if err != nil {
		t.Fatalf("read control-plane E2E script: %v", err)
	}
	script := string(contents)
	for description, expected := range map[string]string{
		"uses a unique Compose project":        `compose_project="fruto-control-plane-e2e-$PPID"`,
		"allocates host ports dynamically":     `allocate_port()`,
		"uses isolated control-plane images":   `api_image="fruto-control-plane-api:local-$PPID"`,
		"injects the bootstrap hash by Secret": `--from=secret/control-plane-bootstrap-owner`,
		"projects the isolated API image":      `image: $api_image`,
		"projects the isolated Console image":  `image: $console_image`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("expected control-plane E2E to %s", description)
		}
	}
	if strings.Contains(script, `set env deployment/control-plane-api FRUTO_OWNER_PASSWORD_HASH="$owner_hash"`) {
		t.Error("control-plane E2E exposes the bootstrap password hash in the PodSpec")
	}
	if strings.Contains(script, `get jobs,pods,httproutes -o yaml`) {
		t.Error("control-plane E2E dumps PodSpecs while collecting diagnostics")
	}
}

func TestGeneratedGateIncludesTheFrontendAPIClient(t *testing.T) {
	contents, err := os.ReadFile("../generated/check.sh")
	if err != nil {
		t.Fatalf("read generated gate: %v", err)
	}
	if !strings.Contains(string(contents), `apps/console-web/src/shared/api/generated/control-plane.ts`) {
		t.Fatal("generated gate does not detect stale frontend API types")
	}
}

func TestControlPlaneManifestsApplyLeastPrivilegeDefaults(t *testing.T) {
	migration, err := os.ReadFile("../../deploy/control-plane/migration-job.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migration), "serviceAccountName: control-plane-api") || !strings.Contains(string(migration), "automountServiceAccountToken: false") {
		t.Error("migration Job still mounts the control-plane API service account token")
	}
	deployments, err := os.ReadFile("../../deploy/control-plane/deployments.yaml")
	if err != nil {
		t.Fatal(err)
	}
	consoleStart := strings.Index(string(deployments), "name: console-web")
	if consoleStart < 0 || !strings.Contains(string(deployments)[consoleStart:], "automountServiceAccountToken: false") {
		t.Error("console Deployment does not disable service account token automount")
	}
	operator, err := os.ReadFile("../../deploy/operator/manager/deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(operator), "image: fruto-platform-operator:e2e") {
		t.Error("operator installation still uses a mutable image tag")
	}
}

func TestCIGateRequiresPostgreSQLBackedControlPlaneTests(t *testing.T) {
	contents, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	justfile := string(contents)

	ciStart := strings.Index(justfile, "\nci:\n")
	if ciStart < 0 {
		t.Fatal("expected canonical ci recipe")
	}
	ciRecipe := justfile[ciStart:]
	if !strings.Contains(ciRecipe, "just control-plane-integration-test") {
		t.Fatal("canonical ci recipe omits the PostgreSQL-backed control-plane integration stage")
	}
	integrationStart := strings.Index(justfile, "\ncontrol-plane-integration-test:\n")
	if integrationStart < 0 || !strings.Contains(justfile[integrationStart:ciStart], "FRUTO_TEST_DATABASE_URL") {
		t.Fatal("canonical ci recipe can pass without enabling the PostgreSQL-backed control-plane tests")
	}
}

func TestE2ECoversFrontendImageContracts(t *testing.T) {
	contents, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatalf("read E2E script: %v", err)
	}
	script := string(contents)

	checks := map[string]string{
		"builds the static HTML image":              "test/fixtures/static-html/Dockerfile",
		"builds the Vite React SPA image":           "test/fixtures/vite-react-spa/Dockerfile",
		"starts the static frontend as private":     "unexpected HTTPRoute for the private static frontend",
		"promotes the static frontend to public":    `"slug":"phase4-static"`,
		"checks a public SPA deep link":             `"${spa_public_url}/projects/example"`,
		"waits for the SPA dataplane":               "wait_for_public_status 200 phase4-spa.fruto.calouro.tech",
		"does not fall back for missing SPA assets": `"${spa_public_url}/assets/missing.js"`,
		"rolls out a second immutable SPA release":  "SPA_IMAGE_V2",
		"observes v2 through the dataplane":         "wait_for_public_content",
		"preserves logical identities on rollout":   "SPA rollout replaced a logical Kubernetes child",
		"garbage collects frontend children":        `--for=delete "${child}/ap-spa000001"`,
	}
	for description, expected := range checks {
		if !strings.Contains(script, expected) {
			t.Errorf("expected E2E to %s", description)
		}
	}
}

func TestE2EFrontendImagesAreIsolatedAndRemoved(t *testing.T) {
	contents, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatalf("read E2E script: %v", err)
	}
	script := string(contents)

	images := []string{"STATIC_IMAGE_TAG", "SPA_IMAGE_V1_TAG", "SPA_IMAGE_V2_TAG"}
	finishStart := strings.Index(script, "finish() {")
	finishEnd := strings.Index(script, "trap finish EXIT")
	if finishStart < 0 || finishEnd <= finishStart {
		t.Fatal("expected E2E script to define the finish trap")
	}
	finishBody := script[finishStart:finishEnd]
	for _, image := range images {
		declaration := `readonly ` + image + `=`
		declarationStart := strings.Index(script, declaration)
		if declarationStart < 0 {
			t.Errorf("expected declaration for %s", image)
			continue
		}
		declarationEnd := strings.Index(script[declarationStart:], "\n")
		if declarationEnd < 0 || !strings.Contains(script[declarationStart:declarationStart+declarationEnd], "$$") {
			t.Errorf("expected %s to use a per-run tag", image)
		}
		if !strings.Contains(finishBody, `"${`+image+`}"`) {
			t.Errorf("expected cleanup to remove %s", image)
		}
	}
}

func TestDockerContextExcludesLocalFrontendArtifacts(t *testing.T) {
	contents, err := os.ReadFile("../../.dockerignore")
	if err != nil {
		t.Fatalf("read .dockerignore: %v", err)
	}
	ignore := string(contents)

	for _, artifact := range []string{
		"test/fixtures/vite-react-spa/node_modules/",
		"test/fixtures/vite-react-spa/dist/",
	} {
		if !strings.Contains(ignore, artifact) {
			t.Errorf("expected Docker context to exclude %s", artifact)
		}
	}
}

func TestE2EProvesFrontendServiceDriftWasRemoved(t *testing.T) {
	contents, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatalf("read E2E script: %v", err)
	}
	script := string(contents)

	start := strings.Index(script, `patch service/ap-static000001`)
	end := strings.Index(script, `create namespace ws-spa-e2e`)
	if start < 0 || end <= start {
		t.Fatal("expected static frontend drift validation section")
	}
	if !strings.Contains(script[start:end], `.spec.selector.drift`) {
		t.Error("expected E2E to assert that the injected Service selector drift was removed")
	}
}

func TestE2EWaitsForCurrentGenerationAfterPublicPromotion(t *testing.T) {
	contents, err := os.ReadFile("run.sh")
	if err != nil {
		t.Fatalf("read E2E script: %v", err)
	}
	script := string(contents)

	start := strings.Index(script, `"slug":"phase4-static"`)
	end := strings.Index(script, `static_public_url=`)
	if start < 0 || end <= start {
		t.Fatal("expected static frontend public promotion section")
	}
	promotion := script[start:end]
	if !strings.Contains(promotion, `.status.observedGeneration`) {
		t.Error("expected E2E to wait until the public generation was observed")
	}
	if !strings.Contains(promotion, `--for=condition=Ready`) {
		t.Error("expected E2E to validate Ready after observing the public generation")
	}
}
