package contracts

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestControlPlaneWorkloadsKeepLeastPrivilegeAndImmutableImages(t *testing.T) {
	tests := []struct {
		path, kind, name, container string
		requireAutomountFalse       bool
	}{
		{"deploy/control-plane/migration-job.yaml", "Job", "control-plane-migrate", "migrate", true},
		{"deploy/control-plane/bootstrap-job.yaml", "Job", "control-plane-bootstrap", "bootstrap", true},
		{"deploy/control-plane/deployments.yaml", "Deployment", "console-web", "web", true},
		{"deploy/operator/manager/deployment.yaml", "Deployment", "platform-operator", "manager", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := findObject(t, tt.path, tt.kind, tt.name)
			podSpec := workloadPodSpec(t, object)
			if value, found, err := unstructured.NestedBool(podSpec, "automountServiceAccountToken"); tt.requireAutomountFalse && (err != nil || !found || value) {
				t.Fatalf("automountServiceAccountToken=%v found=%v err=%v", value, found, err)
			}
			container := findContainer(t, podSpec, tt.container)
			image, _, _ := unstructured.NestedString(container, "image")
			if !strings.Contains(image, "@sha256:") {
				t.Fatalf("container image is not digest-pinned: %q", image)
			}
		})
	}

	migration := findContainer(t, workloadPodSpec(t, findObject(t, "deploy/control-plane/migration-job.yaml", "Job", "control-plane-migrate")), "migrate")
	command, _, _ := unstructured.NestedStringSlice(migration, "command")
	args, _, _ := unstructured.NestedStringSlice(migration, "args")
	if !reflect.DeepEqual(command, []string{"/control-plane-api"}) || !reflect.DeepEqual(args, []string{"migrate"}) {
		t.Fatalf("migration command=%v args=%v", command, args)
	}
}

func TestControlPlaneRouteTargetsTheMolejoGateway(t *testing.T) {
	route := findObject(t, "deploy/control-plane/http-route.yaml", "HTTPRoute", "control-plane-console")
	hostnames, _, _ := unstructured.NestedStringSlice(route.Object, "spec", "hostnames")
	if !reflect.DeepEqual(hostnames, []string{"cloud.molejo.dev"}) {
		t.Fatalf("route hostnames=%v", hostnames)
	}
	parents, _, _ := unstructured.NestedSlice(route.Object, "spec", "parentRefs")
	if len(parents) != 1 {
		t.Fatalf("route parentRefs=%v", parents)
	}
	parent, ok := parents[0].(map[string]any)
	if !ok || parent["name"] != "fruto" || parent["namespace"] != "fruto-system" || parent["sectionName"] != "https-molejo" {
		t.Fatalf("route parentRef=%v", parents[0])
	}
}

func TestLabPostgresIsExplicitlyDisposableAndBounded(t *testing.T) {
	statefulSet := findObject(t, "deploy/control-plane-lab/postgres.yaml", "StatefulSet", "fruto-control-plane-postgres")
	if statefulSet.GetAnnotations()["molejo.dev/disposable"] != "true" {
		t.Fatal("lab PostgreSQL is not marked disposable")
	}
	templates, _, _ := unstructured.NestedSlice(statefulSet.Object, "spec", "volumeClaimTemplates")
	if len(templates) != 1 {
		t.Fatalf("volumeClaimTemplates=%v", templates)
	}
	template := templates[0].(map[string]any)
	requests, _, _ := unstructured.NestedStringMap(template, "spec", "resources", "requests")
	if requests["storage"] != "2Gi" {
		t.Fatalf("lab PostgreSQL storage=%q", requests["storage"])
	}
	storageClass, _, _ := unstructured.NestedString(template, "spec", "storageClassName")
	if storageClass != "molejo-platform-local" {
		t.Fatalf("lab PostgreSQL storageClassName=%q", storageClass)
	}
}

func TestLabParameterWorkerUsesThePrivateRegistryCredential(t *testing.T) {
	worker := findObject(t, "deploy/control-plane-lab/private-registry.yaml", "Deployment", "control-plane-parameter-worker")
	podSpec := workloadPodSpec(t, worker)
	pullSecrets, found, err := unstructured.NestedSlice(podSpec, "imagePullSecrets")
	if err != nil || !found || len(pullSecrets) != 1 {
		t.Fatalf("imagePullSecrets=%v found=%v err=%v", pullSecrets, found, err)
	}
	secret, ok := pullSecrets[0].(map[string]any)
	if !ok || secret["name"] != "registry-pull" {
		t.Fatalf("imagePullSecrets=%v", pullSecrets)
	}
}

func TestPasswordResetSecretParticipatesInTheReleaseContract(t *testing.T) {
	required := map[string][]string{
		"deploy/control-plane/deployments.yaml":    {"required-external-password-reset-secret", "FRUTO_PASSWORD_RESET_KEY_FILE"},
		"test/e2e/prepare-control-plane-k3s.sh":    {"MOLEJO_PASSWORD_RESET_SECRET", "apply_versioned_object", "password-reset-key"},
		"test/e2e/render-control-plane-release.sh": {"required-external-password-reset-secret", "MOLEJO_PASSWORD_RESET_SECRET"},
		"test/e2e/apply-control-plane-k3s.sh":      {"MOLEJO_PASSWORD_RESET_SECRET"},
	}
	for path, fragments := range required {
		contents, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(contents), fragment) {
				t.Fatalf("%s is missing release contract fragment %q", path, fragment)
			}
		}
	}
}

func TestControlPlaneReleaseIncludesTheOperatorAndStatefulContracts(t *testing.T) {
	required := map[string][]string{
		"deploy/control-plane-release/kustomization.yaml": {"../crds", "../operator", "../control-plane-lab", "stateful-scheduling-lab.yaml"},
		"test/e2e/build-control-plane-release.sh":         {"FRUTO_OPERATOR_IMAGE", "services/platform-operator/Dockerfile", "operator.json"},
		"test/e2e/render-control-plane-release.sh":        {"FRUTO_OPERATOR_IMAGE", "control-plane-release", "platform-operator@sha256"},
		"test/e2e/apply-control-plane-k3s.sh":             {"appvolumes.platform.fruto.calouro.tech", "deployment/platform-operator"},
		"test/e2e/accept-control-plane-k3s.sh":            {"FRUTO_OPERATOR_IMAGE", "persistent-standard", "molejo-app-local", "--subresource=status"},
	}
	for path, fragments := range required {
		contents, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(contents), fragment) {
				t.Fatalf("%s is missing release contract fragment %q", path, fragment)
			}
		}
	}
}

func TestLabReleaseConfiguresStatefulSchedulingWithoutExposingItInTheCRD(t *testing.T) {
	operator := findObject(t, "deploy/control-plane-release/stateful-scheduling-lab.yaml", "Deployment", "platform-operator")
	container := findContainer(t, workloadPodSpec(t, operator), "manager")
	environment, found, err := unstructured.NestedSlice(container, "env")
	if err != nil || !found {
		t.Fatalf("operator env=%v found=%v err=%v", environment, found, err)
	}
	for _, item := range environment {
		variable, ok := item.(map[string]any)
		if !ok || variable["name"] != "MOLEJO_STATEFUL_TOLERATIONS_JSON" {
			continue
		}
		value, _ := variable["value"].(string)
		for _, required := range []string{"fruto.cleidsonoliveira.dev/workload", "data", "NoSchedule"} {
			if !strings.Contains(value, required) {
				t.Fatalf("stateful scheduling value %q is missing %q", value, required)
			}
		}
		return
	}
	t.Fatal("lab release does not configure stateful scheduling")
}

func TestObservabilityPlaneIsInternalBoundedAndDigestPinned(t *testing.T) {
	workloads := []struct{ kind, name, container string }{
		{"StatefulSet", "clickhouse", "clickhouse"},
		{"StatefulSet", "victoria-metrics", "victoria-metrics"},
		{"Deployment", "otel-gateway", "collector"},
		{"Deployment", "otel-cluster", "collector"},
		{"DaemonSet", "otel-agent", "collector"},
	}
	for _, workload := range workloads {
		t.Run(workload.name, func(t *testing.T) {
			object := findObject(t, "deploy/observability/workloads.yaml", workload.kind, workload.name)
			container := findContainer(t, workloadPodSpec(t, object), workload.container)
			image, _, _ := unstructured.NestedString(container, "image")
			if !strings.Contains(image, "@sha256:") {
				t.Fatalf("container image is not digest-pinned: %q", image)
			}
			requests, found, err := unstructured.NestedStringMap(container, "resources", "requests")
			if err != nil || !found || requests["cpu"] == "" || requests["memory"] == "" {
				t.Fatalf("resource requests=%v found=%v err=%v", requests, found, err)
			}
		})
	}

	for _, name := range []string{"clickhouse", "victoria-metrics", "otel-gateway"} {
		service := findObject(t, "deploy/observability/services.yaml", "Service", name)
		serviceType, found, err := unstructured.NestedString(service.Object, "spec", "type")
		if err != nil || found && serviceType != "ClusterIP" {
			t.Fatalf("service %s exposes type %q", name, serviceType)
		}
	}

	for _, name := range []string{"clickhouse", "victoria-metrics"} {
		statefulSet := findObject(t, "deploy/observability/workloads.yaml", "StatefulSet", name)
		templates, found, err := unstructured.NestedSlice(statefulSet.Object, "spec", "volumeClaimTemplates")
		if err != nil || !found || len(templates) != 1 {
			t.Fatalf("%s volumeClaimTemplates=%v found=%v err=%v", name, templates, found, err)
		}
		template := templates[0].(map[string]any)
		storageClass, _, _ := unstructured.NestedString(template, "spec", "storageClassName")
		if storageClass != "molejo-platform-local" {
			t.Fatalf("%s storageClassName=%q", name, storageClass)
		}
	}
}

func TestObservabilityIngestionIsRuntimeScopedAndQueriesAreBounded(t *testing.T) {
	config, err := os.ReadFile(filepath.Join("..", "..", "deploy/observability/config/otel-agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	for _, required := range []string{
		"filter/runtime",
		`resource.attributes["molejo.app_environment.runtime"] == nil`,
		"processors: [memory_limiter, k8sattributes, filter/runtime, batch]",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("observability collector config is missing %q", required)
		}
	}

	workloads, err := os.ReadFile(filepath.Join("..", "..", "deploy/observability/workloads.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text = string(workloads)
	for _, required := range []string{
		"- -retentionPeriod=35d",
		"- -search.maxQueryDuration=10s",
		"- -search.maxResponseSeries=100",
		"- -search.maxPointsPerTimeseries=1000",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("VictoriaMetrics workload is missing %q", required)
		}
	}

	migration, err := os.ReadFile(filepath.Join("..", "..", "deploy/observability/log-schema-migration.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	migrationText := string(migration)
	for _, required := range []string{
		"MODIFY COLUMN MolejoRuntime",
		"MATERIALIZED ResourceAttributes['molejo.app_environment.runtime']",
		"MATERIALIZE COLUMN MolejoRuntime",
		`runtime_changed=1`,
		`materialize_runtime_index="$runtime_changed"`,
		"MATERIALIZE INDEX MolejoRuntimeIndex",
	} {
		if !strings.Contains(migrationText, required) {
			t.Fatalf("ClickHouse runtime correlation migration is missing %q", required)
		}
	}
	if strings.Contains(migrationText, "MATERIALIZED ResourceAttributes['k8s.deployment.name']") {
		t.Fatal("ClickHouse runtime correlation is coupled to Deployments")
	}
}

func TestOpenEBSCapacityScrapeDropsVolumeCardinality(t *testing.T) {
	config, err := os.ReadFile(filepath.Join("..", "..", "deploy/observability/config/otel-cluster.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	for _, required := range []string{
		"job_name: openebs-lvm-capacity",
		"openebs-lvm-lvm-localpv-node-service.openebs.svc:9500",
		"lvm_vg_(free_size_bytes|total_size_bytes|missing_pv_count|lv_count)",
		"target_label: volume_group",
		"regex: name",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("OpenEBS capacity scrape is missing %q", required)
		}
	}
	for _, forbidden := range []string{"lvm_lv_", "openebs_size_of_volume", "volumename"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("OpenEBS capacity scrape exposes high-cardinality metric fragment %q", forbidden)
		}
	}
	network, err := os.ReadFile(filepath.Join("..", "..", "deploy/observability/network-policies.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"kubernetes.io/metadata.name: openebs", "app: openebs-lvm-node", "port: 9500"} {
		if !strings.Contains(string(network), required) {
			t.Fatalf("OpenEBS capacity scrape network policy is missing %q", required)
		}
	}
}

func findObject(t *testing.T, relativePath, kind, name string) *unstructured.Unstructured {
	t.Helper()
	path := filepath.Join("..", "..", relativePath)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	for {
		object := &unstructured.Unstructured{}
		if err = decoder.Decode(object); err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if object.GetKind() == kind && object.GetName() == name {
			return object
		}
	}
	t.Fatalf("%s %s not found in %s", kind, name, relativePath)
	return nil
}

func workloadPodSpec(t *testing.T, object *unstructured.Unstructured) map[string]any {
	t.Helper()
	path := []string{"spec", "template", "spec"}
	podSpec, found, err := unstructured.NestedMap(object.Object, path...)
	if err != nil || !found {
		t.Fatalf("pod spec not found: found=%v err=%v", found, err)
	}
	return podSpec
}

func findContainer(t *testing.T, podSpec map[string]any, name string) map[string]any {
	t.Helper()
	containers, found, err := unstructured.NestedSlice(podSpec, "containers")
	if err != nil || !found {
		t.Fatalf("containers not found: found=%v err=%v", found, err)
	}
	for _, item := range containers {
		container := item.(map[string]any)
		if container["name"] == name {
			return container
		}
	}
	t.Fatalf("container %s not found", name)
	return nil
}
