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

func TestControlPlaneGenerationIncludesServerAndSQLC(t *testing.T) {
	justContents, err := os.ReadFile("../../justfile")
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	generatedCheck, err := os.ReadFile("../generated/check.sh")
	if err != nil {
		t.Fatalf("read generated check: %v", err)
	}

	for _, expected := range []string{"go tool oapi-codegen", "go tool sqlc generate"} {
		if !strings.Contains(string(justContents), expected) {
			t.Errorf("just generate is missing %q", expected)
		}
	}
	for _, expected := range []string{
		"services/control-plane-api/internal/api/generated/control-plane.gen.go",
		"services/control-plane-api/internal/store/sqlc/db.go",
		"services/control-plane-api/internal/store/sqlc/models.go",
		"services/control-plane-api/internal/store/sqlc/querier.go",
	} {
		if !strings.Contains(string(generatedCheck), expected) {
			t.Errorf("generated gate is missing %q", expected)
		}
	}
}

func TestControlPlaneUsesPinnedGooseAndKeepsSQLCTypesInsideTheStore(t *testing.T) {
	storeContents, err := os.ReadFile("../../services/control-plane-api/internal/store/store.go")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(storeContents)
	for _, expected := range []string{"storesqlc.New(pool)", "queries.WithTx(tx)", "goose.NewProvider", "goose.WithSessionLocker"} {
		if !strings.Contains(contents, expected) {
			t.Errorf("store is missing %q", expected)
		}
	}
	module, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"github.com/pressly/goose/v3/cmd/goose", "github.com/sqlc-dev/sqlc/cmd/sqlc"} {
		if !strings.Contains(string(module), expected) {
			t.Errorf("go.mod does not pin %q", expected)
		}
	}
	apiEntries, err := os.ReadDir("../../services/control-plane-api/internal/api")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range apiEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		apiFile, err := os.ReadFile("../../services/control-plane-api/internal/api/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(apiFile), "/internal/store/sqlc") {
			t.Errorf("SQLC type leaked into HTTP adapter %s", entry.Name())
		}
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

func TestControlPlanePhase7ArtifactsAreExplicitAndReproducible(t *testing.T) {
	preflight, err := os.ReadFile("control-plane-k3s-preflight.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`FRUTO_CERTIFICATE_NAMESPACE:-traefik-system`,
		`FRUTO_CERTIFICATE_NAME:-molejo-public-tls`,
		`FRUTO_CERTIFICATE_SECRET:-molejo-public-tls`,
	} {
		if !strings.Contains(string(preflight), expected) {
			t.Errorf("k3s preflight is missing %q", expected)
		}
	}

	route, err := os.ReadFile("../../deploy/control-plane/http-route.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"name: control-plane-console", "sectionName: https-molejo", "cloud.molejo.dev"} {
		if !strings.Contains(string(route), expected) {
			t.Errorf("control-plane route is missing %q", expected)
		}
	}
	for _, forbidden := range []string{"control-plane-redirect", "type: RequestRedirect"} {
		if strings.Contains(string(route), forbidden) {
			t.Errorf("control-plane route assumes foundation-owned redirect %q", forbidden)
		}
	}

	bootstrap, err := os.ReadFile("../../deploy/control-plane/bootstrap-job.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"args: [\"bootstrap\"]", "name: fruto-control-plane-bootstrap", "automountServiceAccountToken: false"} {
		if !strings.Contains(string(bootstrap), expected) {
			t.Errorf("bootstrap Job is missing %q", expected)
		}
	}

	lab, err := os.ReadFile("../../deploy/control-plane-lab/postgres.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`molejo.dev/disposable: "true"`,
		"kind: StatefulSet",
		"name: fruto-control-plane-postgres",
		"storage: 2Gi",
		"fruto.cleidsonoliveira.dev/workload",
		"value: data",
		"effect: NoSchedule",
	} {
		if !strings.Contains(string(lab), expected) {
			t.Errorf("lab PostgreSQL fixture is missing %q", expected)
		}
	}

	renderer, err := os.ReadFile("render-control-plane-release.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"FRUTO_TESTKIT_IMAGE", "testkit=%s", "deploy/control-plane-lab"} {
		if !strings.Contains(string(renderer), expected) {
			t.Errorf("release renderer is missing %q", expected)
		}
	}

	for path, assertions := range map[string][]string{
		"build-control-plane-release.sh": {
			"--platform linux/amd64", "--push", `containerimage.digest`, "images.env",
		},
		"prepare-control-plane-k3s.sh": {
			"openssl rand", "hash-password", "fruto-control-plane-bootstrap", "registry-pull",
		},
		"apply-control-plane-k3s.sh": {
			"--dry-run=server", "control-plane-migrate", "control-plane-bootstrap", "control-plane-console",
		},
		"accept-control-plane-k3s.sh": {
			"http://cloud.molejo.dev/", "ResolvedRefs", "auth can-i", "ssl_verify_result",
		},
	} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		for _, expected := range assertions {
			if !strings.Contains(string(contents), expected) {
				t.Errorf("%s is missing %q", path, expected)
			}
		}
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

func TestControlPlaneApplyDryRunExcludesImmutableJobs(t *testing.T) {
	contents, err := os.ReadFile("apply-control-plane-k3s.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(contents)
	if strings.Contains(script, `--dry-run=server -f "$release_file"`) {
		t.Fatal("server-side dry-run attempts to update immutable Jobs from the complete release bundle")
	}
	for _, expected := range []string{`select(.kind != "Job")`, `--dry-run=server -f -`} {
		if !strings.Contains(script, expected) {
			t.Errorf("server-side dry-run is missing %q", expected)
		}
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
		"waits for the SPA dataplane":               "wait_for_public_status 200 phase4-spa.molejo.dev",
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

func TestPublicHostnamesUseTheMolejoDomain(t *testing.T) {
	files := map[string]struct {
		expected  []string
		forbidden []string
	}{
		"../../deploy/control-plane/configmap.yaml": {
			expected: []string{"https://cloud.molejo.dev"}, forbidden: []string{"console.fruto.calouro.tech"},
		},
		"../../deploy/control-plane/http-route.yaml": {
			expected: []string{"cloud.molejo.dev", "sectionName: https-molejo"}, forbidden: []string{"console.fruto.calouro.tech"},
		},
		"control-plane-kind.sh": {
			expected: []string{"cloud.molejo.dev", "*.molejo.dev"}, forbidden: []string{"console.fruto.calouro.tech", "*.fruto.calouro.tech"},
		},
		"gateway.yaml": {
			expected: []string{"listeners:\n    - name: https-molejo", `hostname: "*.molejo.dev"`}, forbidden: []string{`hostname: "*.fruto.calouro.tech"`},
		},
		"run.sh": {
			expected:  []string{"phase3-e2e.molejo.dev", "phase4-static.molejo.dev", "phase4-spa.molejo.dev"},
			forbidden: []string{"phase3-e2e.fruto.calouro.tech", "phase4-static.fruto.calouro.tech", "phase4-spa.fruto.calouro.tech", "*.fruto.calouro.tech"},
		},
	}
	for path, assertions := range files {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, expected := range assertions.expected {
			if !strings.Contains(string(contents), expected) {
				t.Errorf("expected %s to contain %q", path, expected)
			}
		}
		for _, forbidden := range assertions.forbidden {
			if strings.Contains(string(contents), forbidden) {
				t.Errorf("expected %s to stop using retired public hostname %q", path, forbidden)
			}
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
