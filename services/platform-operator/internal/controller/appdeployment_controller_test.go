package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

const (
	testImage  = "registry.k8s.io/pause:3.10.1@sha256:278fb9dbcca9518083ad1e11276933a2e96f23de604a3a08cc3c80002767d24c"
	otherImage = "registry.example.test/app@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestAppDeploymentSchema(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "schema")

	t.Run("accepts a digest and defaults replicas", func(t *testing.T) {
		appDeployment := newAppDeployment(namespace, "ap-valid0001", testImage)
		if err := testClient.Create(ctx, appDeployment); err != nil {
			t.Fatalf("create valid AppDeployment: %v", err)
		}
		if appDeployment.Spec.Replicas == nil || *appDeployment.Spec.Replicas != 1 {
			t.Fatalf("expected replicas to default to 1, got %v", appDeployment.Spec.Replicas)
		}
	})

	t.Run("retains the private runtime contract", func(t *testing.T) {
		dynamicClient, err := dynamic.NewForConfig(testConfig)
		if err != nil {
			t.Fatalf("create dynamic client: %v", err)
		}
		resource := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "platform.molejo.dev/v1alpha1",
			"kind":       "AppDeployment",
			"metadata": map[string]any{
				"name":      "ap-runtimecontract",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"workload": map[string]any{"kind": "Stateless", "stateless": map[string]any{}},
				"image":    testImage,
				"ports":    []any{map[string]any{"name": "http", "containerPort": int64(8080), "protocol": "TCP"}},
				"resources": map[string]any{
					"requests": map[string]any{"cpuMillis": int64(50), "memoryMiB": int64(64)},
					"limits":   map[string]any{"cpuMillis": int64(500), "memoryMiB": int64(256)},
				},
				"probes": map[string]any{
					"startup":   map[string]any{"type": "HTTP", "portName": "http", "path": "/readyz"},
					"liveness":  map[string]any{"type": "HTTP", "portName": "http", "path": "/healthz"},
					"readiness": map[string]any{"type": "HTTP", "portName": "http", "path": "/readyz"},
				},
			},
		}}
		gvr := schema.GroupVersionResource{
			Group: "platform.molejo.dev", Version: "v1alpha1", Resource: "appdeployments",
		}

		created, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(
			ctx,
			resource,
			metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict},
		)
		if err != nil {
			t.Fatalf("create AppDeployment with private runtime contract: %v", err)
		}
		for _, fieldPath := range [][]string{
			{"spec", "ports"},
			{"spec", "resources"},
			{"spec", "probes"},
		} {
			if _, found, err := unstructured.NestedFieldNoCopy(created.Object, fieldPath...); err != nil {
				t.Fatalf("read %s: %v", strings.Join(fieldPath, "."), err)
			} else if !found {
				t.Fatalf("expected API server to retain %s", strings.Join(fieldPath, "."))
			}
		}
	})

	tests := []struct {
		name         string
		resourceName string
		image        string
		replicas     *int32
	}{
		{
			name:         "rejects an ID without the ap prefix",
			resourceName: "invalid-id",
			image:        testImage,
		},
		{
			name:         "rejects a mutable image tag",
			resourceName: "ap-invalidimage",
			image:        "registry.k8s.io/pause:3.10.1",
		},
		{
			name:         "rejects zero replicas",
			resourceName: "ap-invalidreplicas",
			image:        testImage,
			replicas:     pointerTo(int32(0)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			appDeployment := newAppDeployment(namespace, test.resourceName, test.image)
			appDeployment.Spec.Replicas = test.replicas
			if err := testClient.Create(ctx, appDeployment); err == nil {
				t.Fatal("expected API validation to reject AppDeployment")
			}
		})
	}

	runtimeTests := []struct {
		name   string
		mutate func(*platformv1alpha1.AppDeployment)
	}{
		{
			name: "rejects missing ports",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Ports = nil
			},
		},
		{
			name: "rejects a port above 65535",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Ports[0].ContainerPort = 65536
			},
		},
		{
			name: "rejects zero resource values",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Resources.Requests.CPUMillis = 0
			},
		},
		{
			name: "rejects CPU requests above limits",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Resources.Requests.CPUMillis = 501
			},
		},
		{
			name: "rejects memory requests above limits",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Resources.Requests.MemoryMiB = 257
			},
		},
		{
			name: "rejects a relative liveness path",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Probes.Liveness.Path = "healthz"
			},
		},
		{
			name: "rejects a missing readiness path",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Probes.Readiness.Path = ""
			},
		},
	}
	for testIndex, test := range runtimeTests {
		t.Run(test.name, func(t *testing.T) {
			appDeployment := newAppDeployment(
				namespace,
				fmt.Sprintf("ap-invalidruntime%02d", testIndex),
				testImage,
			)
			test.mutate(appDeployment)
			if err := testClient.Create(ctx, appDeployment); err == nil {
				t.Fatal("expected API validation to reject the runtime contract")
			}
		})
	}

	t.Run("rejects the removed port field in strict mode", func(t *testing.T) {
		dynamicClient, err := dynamic.NewForConfig(testConfig)
		if err != nil {
			t.Fatalf("create dynamic client: %v", err)
		}
		resource := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "platform.molejo.dev/v1alpha1",
			"kind":       "AppDeployment",
			"metadata": map[string]any{
				"name":      "ap-unknownfield",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"workload": map[string]any{"kind": "Stateless", "stateless": map[string]any{}},
				"image":    testImage,
				"ports":    []any{map[string]any{"name": "http", "containerPort": int64(8080), "protocol": "TCP"}},
				"resources": map[string]any{
					"requests": map[string]any{"cpuMillis": int64(50), "memoryMiB": int64(64)},
					"limits":   map[string]any{"cpuMillis": int64(500), "memoryMiB": int64(256)},
				},
				"probes": map[string]any{
					"startup":   map[string]any{"type": "HTTP", "portName": "http", "path": "/readyz"},
					"liveness":  map[string]any{"type": "HTTP", "portName": "http", "path": "/healthz"},
					"readiness": map[string]any{"type": "HTTP", "portName": "http", "path": "/readyz"},
				},
				"port": int64(8080),
			},
		}}
		gvr := schema.GroupVersionResource{
			Group: "platform.molejo.dev", Version: "v1alpha1", Resource: "appdeployments",
		}
		_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, resource, metav1.CreateOptions{
			FieldValidation: metav1.FieldValidationStrict,
		})
		if err == nil {
			t.Fatal("expected strict field validation to reject the removed port field")
		}
	})

	t.Run("accepts and prunes an unknown field outside strict mode", func(t *testing.T) {
		dynamicClient, err := dynamic.NewForConfig(testConfig)
		if err != nil {
			t.Fatalf("create dynamic client: %v", err)
		}
		resource := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "platform.molejo.dev/v1alpha1",
			"kind":       "AppDeployment",
			"metadata": map[string]any{
				"name":      "ap-unknownfieldnonstrict",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"workload": map[string]any{"kind": "Stateless", "stateless": map[string]any{}},
				"image":    testImage,
				"ports":    []any{map[string]any{"name": "http", "containerPort": int64(8080), "protocol": "TCP"}},
				"resources": map[string]any{
					"requests": map[string]any{"cpuMillis": int64(50), "memoryMiB": int64(64)},
					"limits":   map[string]any{"cpuMillis": int64(500), "memoryMiB": int64(256)},
				},
				"probes": map[string]any{
					"startup":   map[string]any{"type": "HTTP", "portName": "http", "path": "/readyz"},
					"liveness":  map[string]any{"type": "HTTP", "portName": "http", "path": "/healthz"},
					"readiness": map[string]any{"type": "HTTP", "portName": "http", "path": "/readyz"},
				},
				"unexpected": true,
			},
		}}
		gvr := schema.GroupVersionResource{
			Group: "platform.molejo.dev", Version: "v1alpha1", Resource: "appdeployments",
		}

		created, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(
			ctx,
			resource,
			metav1.CreateOptions{},
		)
		if err != nil {
			t.Fatalf("expected non-strict request to be accepted: %v", err)
		}
		if _, found, err := unstructured.NestedBool(created.Object, "spec", "unexpected"); err != nil {
			t.Fatalf("read unknown field from created resource: %v", err)
		} else if found {
			t.Fatal("expected the API server to prune the unknown field")
		}
	})
}

func TestReconcileSkipsTerminatingAppDeployment(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "terminating")
	appDeployment := newAppDeployment(namespace, "ap-terminating0001", testImage)
	appDeployment.Finalizers = []string{"test.molejo.dev/hold"}
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}
	if err := testClient.Delete(ctx, appDeployment); err != nil {
		t.Fatalf("start AppDeployment deletion: %v", err)
	}

	key := client.ObjectKeyFromObject(appDeployment)
	terminating := getAppDeployment(t, ctx, key)
	if terminating.DeletionTimestamp.IsZero() {
		t.Fatal("expected AppDeployment to remain terminating behind the test finalizer")
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile terminating AppDeployment: %v", err)
	}

	for kind, object := range map[string]client.Object{
		"Deployment": &appsv1.Deployment{},
		"Service":    &corev1.Service{},
	} {
		err := testClient.Get(ctx, key, object)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected terminating AppDeployment not to create %s, got %v", kind, err)
		}
	}
}

func TestStatusPatchRejectsAStaleWriter(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "status-conflict")
	appDeployment := newAppDeployment(namespace, "ap-statusconflict", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	key := client.ObjectKeyFromObject(appDeployment)
	staleBefore := getAppDeployment(t, ctx, key)
	currentBefore := getAppDeployment(t, ctx, key)
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}

	currentAfter := currentBefore.DeepCopy()
	currentAfter.Status.ObservedGeneration = currentAfter.Generation
	currentAfter.Status.ObservedRelease = currentAfter.Spec.Image
	applyDecision(currentAfter, workloadDecision{
		state:  workloadStateReady,
		reason: platformv1alpha1.ReasonDeploymentAvailable,
	})
	if err := reconciler.patchStatusIfChanged(ctx, currentBefore, currentAfter); err != nil {
		t.Fatalf("patch current status: %v", err)
	}

	staleAfter := staleBefore.DeepCopy()
	applyDecision(staleAfter, workloadDecision{
		state:  workloadStateDegraded,
		reason: platformv1alpha1.ReasonReplicaFailure,
	})
	err := reconciler.patchStatusIfChanged(ctx, staleBefore, staleAfter)
	if !apierrors.IsConflict(err) {
		t.Fatalf("expected stale status patch to fail with a resourceVersion conflict, got %v", err)
	}

	stored := getAppDeployment(t, ctx, key)
	assertCondition(t, stored, platformv1alpha1.ConditionReady, metav1.ConditionTrue,
		platformv1alpha1.ReasonDeploymentAvailable)
}

func createTestNamespace(t *testing.T, suffix string) string {
	t.Helper()
	startEnvtest(t)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "ws-" + suffix + "-"}}
	if err := testClient.Create(context.Background(), namespace); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	return namespace.Name
}

func newAppDeployment(namespace string, name string, image string) *platformv1alpha1.AppDeployment {
	return &platformv1alpha1.AppDeployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: platformv1alpha1.GroupVersion.String(),
			Kind:       "AppDeployment",
		},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: platformv1alpha1.AppDeploymentSpec{
			Workload: platformv1alpha1.AppDeploymentWorkload{
				Kind:      platformv1alpha1.WorkloadStateless,
				Stateless: &platformv1alpha1.StatelessWorkload{},
			},
			Image:        image,
			Ports:        []platformv1alpha1.AppDeploymentPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
			ConfigMapRef: name + "-c1",
			SecretRef:    name + "-c1-secret",
			Variables: []platformv1alpha1.AppDeploymentVariable{
				{Name: "APP_MODE", Value: "test"},
			},
			Resources: platformv1alpha1.AppDeploymentResources{
				Requests: platformv1alpha1.AppDeploymentResourceValues{CPUMillis: 50, MemoryMiB: 64},
				Limits:   platformv1alpha1.AppDeploymentResourceValues{CPUMillis: 500, MemoryMiB: 256},
			},
			Probes: platformv1alpha1.AppDeploymentProbes{
				Startup:   platformv1alpha1.AppDeploymentProbe{Type: "HTTP", PortName: "http", Path: "/readyz"},
				Liveness:  platformv1alpha1.AppDeploymentProbe{Type: "HTTP", PortName: "http", Path: "/healthz"},
				Readiness: platformv1alpha1.AppDeploymentProbe{Type: "HTTP", PortName: "http", Path: "/readyz"},
			},
		},
	}
}

func getDeployment(t *testing.T, ctx context.Context, key client.ObjectKey) *appsv1.Deployment {
	t.Helper()
	deployment := &appsv1.Deployment{}
	if err := testClient.Get(ctx, key, deployment); err != nil {
		t.Fatalf("get Deployment %s: %v", key, err)
	}
	return deployment
}

func getService(t *testing.T, ctx context.Context, key client.ObjectKey) *corev1.Service {
	t.Helper()
	service := &corev1.Service{}
	if err := testClient.Get(ctx, key, service); err != nil {
		t.Fatalf("get Service %s: %v", key, err)
	}
	return service
}

func assertDeploymentRuntime(
	t *testing.T,
	deployment *appsv1.Deployment,
	spec platformv1alpha1.AppDeploymentSpec,
) {
	t.Helper()
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected exactly one container, got %d", len(deployment.Spec.Template.Spec.Containers))
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if len(container.Env) != len(spec.Variables) || container.Env[0].Name != spec.Variables[0].Name || container.Env[0].Value != spec.Variables[0].Value {
		t.Fatalf("unexpected environment variables: %#v", container.Env)
	}
	if len(container.EnvFrom) != 2 || container.EnvFrom[0].ConfigMapRef == nil || container.EnvFrom[0].ConfigMapRef.Name != spec.ConfigMapRef || container.EnvFrom[1].SecretRef == nil || container.EnvFrom[1].SecretRef.Name != spec.SecretRef {
		t.Fatalf("unexpected environment references: %#v", container.EnvFrom)
	}
	if len(container.Ports) != 1 || container.Ports[0].Name != httpPortName ||
		container.Ports[0].ContainerPort != spec.Ports[0].ContainerPort || container.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("unexpected HTTP container port: %#v", container.Ports)
	}
	if got := container.Resources.Requests.Cpu().MilliValue(); got != spec.Resources.Requests.CPUMillis {
		t.Fatalf("expected CPU request %dm, got %dm", spec.Resources.Requests.CPUMillis, got)
	}
	if got := container.Resources.Limits.Cpu().MilliValue(); got != spec.Resources.Limits.CPUMillis {
		t.Fatalf("expected CPU limit %dm, got %dm", spec.Resources.Limits.CPUMillis, got)
	}
	if got := container.Resources.Requests.Memory().Value(); got != spec.Resources.Requests.MemoryMiB*1024*1024 {
		t.Fatalf("expected memory request %dMi, got %d bytes", spec.Resources.Requests.MemoryMiB, got)
	}
	if got := container.Resources.Limits.Memory().Value(); got != spec.Resources.Limits.MemoryMiB*1024*1024 {
		t.Fatalf("expected memory limit %dMi, got %d bytes", spec.Resources.Limits.MemoryMiB, got)
	}
	assertHTTPProbe(t, "startup", container.StartupProbe, spec.Probes.Startup.Path, 2, 30)
	assertHTTPProbe(t, "readiness", container.ReadinessProbe, spec.Probes.Readiness.Path, 5, 3)
	assertHTTPProbe(t, "liveness", container.LivenessProbe, spec.Probes.Liveness.Path, 10, 3)

	podSecurity := deployment.Spec.Template.Spec.SecurityContext
	if podSecurity == nil || podSecurity.RunAsNonRoot == nil || !*podSecurity.RunAsNonRoot ||
		podSecurity.SeccompProfile == nil ||
		podSecurity.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("expected restricted Pod security context, got %#v", podSecurity)
	}
	security := container.SecurityContext
	if security == nil || security.AllowPrivilegeEscalation == nil || *security.AllowPrivilegeEscalation ||
		security.ReadOnlyRootFilesystem == nil || !*security.ReadOnlyRootFilesystem ||
		security.Capabilities == nil || len(security.Capabilities.Drop) != 1 ||
		security.Capabilities.Drop[0] != corev1.Capability("ALL") {
		t.Fatalf("expected restricted container security context, got %#v", security)
	}
	podSpec := deployment.Spec.Template.Spec
	if podSpec.HostNetwork || podSpec.HostPID || podSpec.HostIPC || len(podSpec.InitContainers) != 0 ||
		len(podSpec.Volumes) != 0 || podSpec.ServiceAccountName != "" ||
		podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken ||
		podSpec.NodeName != "" || podSpec.SchedulerName != corev1.DefaultSchedulerName ||
		len(podSpec.ReadinessGates) != 0 ||
		podSpec.RuntimeClassName != nil || len(podSpec.SchedulingGates) != 0 {
		t.Fatalf("expected the restricted PodSpec surface, got %#v", podSpec)
	}
}

func assertHTTPProbe(
	t *testing.T,
	name string,
	probe *corev1.Probe,
	path string,
	periodSeconds int32,
	failureThreshold int32,
) {
	t.Helper()
	if probe == nil || probe.HTTPGet == nil {
		t.Fatalf("expected %s HTTP probe, got %#v", name, probe)
	}
	if probe.HTTPGet.Path != path || probe.HTTPGet.Port.StrVal != httpPortName ||
		probe.HTTPGet.Scheme != corev1.URISchemeHTTP || probe.TimeoutSeconds != 2 ||
		probe.PeriodSeconds != periodSeconds || probe.SuccessThreshold != 1 ||
		probe.FailureThreshold != failureThreshold {
		t.Fatalf("unexpected %s probe: %#v", name, probe)
	}
}

func assertPrivateService(
	t *testing.T,
	service *corev1.Service,
	appDeployment *platformv1alpha1.AppDeployment,
) {
	t.Helper()
	if !metav1.IsControlledBy(service, appDeployment) {
		t.Fatal("expected Service to be controlled by AppDeployment")
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.ClusterIP == "" {
		t.Fatalf("expected an allocated ClusterIP Service, got type=%s clusterIP=%q",
			service.Spec.Type, service.Spec.ClusterIP)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Name != httpPortName ||
		service.Spec.Ports[0].Protocol != corev1.ProtocolTCP ||
		service.Spec.Ports[0].Port != appDeployment.Spec.Ports[0].ContainerPort ||
		service.Spec.Ports[0].TargetPort.StrVal != httpPortName || service.Spec.Ports[0].NodePort != 0 {
		t.Fatalf("unexpected private Service port: %#v", service.Spec.Ports)
	}
	if service.Spec.Selector[appDeploymentLabel] != appDeployment.Name ||
		len(service.Spec.ExternalIPs) != 0 || service.Spec.ExternalName != "" ||
		service.Spec.LoadBalancerIP != "" || service.Spec.LoadBalancerClass != nil ||
		service.Spec.HealthCheckNodePort != 0 {
		t.Fatalf("unexpected private Service exposure state: %#v", service.Spec)
	}
	if service.Spec.PublishNotReadyAddresses || service.Spec.SessionAffinity != corev1.ServiceAffinityNone ||
		service.Spec.SessionAffinityConfig != nil ||
		(service.Spec.InternalTrafficPolicy != nil &&
			*service.Spec.InternalTrafficPolicy != corev1.ServiceInternalTrafficPolicyCluster) ||
		service.Spec.TrafficDistribution != nil {
		t.Fatalf("unexpected private Service behavior: %#v", service.Spec)
	}
}

func getAppDeployment(t *testing.T, ctx context.Context, key client.ObjectKey) *platformv1alpha1.AppDeployment {
	t.Helper()
	appDeployment := &platformv1alpha1.AppDeployment{}
	if err := testClient.Get(ctx, key, appDeployment); err != nil {
		t.Fatalf("get AppDeployment %s: %v", key, err)
	}
	return appDeployment
}

func assertCondition(
	t *testing.T,
	appDeployment *platformv1alpha1.AppDeployment,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
) {
	t.Helper()
	condition := meta.FindStatusCondition(appDeployment.Status.Conditions, conditionType)
	if condition == nil {
		t.Fatalf("expected condition %s", conditionType)
	}
	if condition.Status != status || condition.Reason != reason {
		t.Fatalf("expected condition %s=%s reason=%s, got status=%s reason=%s",
			conditionType, status, reason, condition.Status, condition.Reason)
	}
}

func pointerTo[T any](value T) *T {
	return &value
}
