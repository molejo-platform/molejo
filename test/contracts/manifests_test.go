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
