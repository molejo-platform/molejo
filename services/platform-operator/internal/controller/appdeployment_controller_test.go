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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
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
			"apiVersion": "platform.fruto.calouro.tech/v1alpha1",
			"kind":       "AppDeployment",
			"metadata": map[string]any{
				"name":      "ap-runtimecontract",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"workload": map[string]any{"kind": "Stateless", "stateless": map[string]any{}},
				"image":    testImage,
				"port":     int64(8080),
				"resources": map[string]any{
					"requests": map[string]any{"cpuMillis": int64(50), "memoryMiB": int64(64)},
					"limits":   map[string]any{"cpuMillis": int64(500), "memoryMiB": int64(256)},
				},
				"probes": map[string]any{
					"liveness":  map[string]any{"path": "/healthz"},
					"readiness": map[string]any{"path": "/readyz"},
				},
			},
		}}
		gvr := schema.GroupVersionResource{
			Group: "platform.fruto.calouro.tech", Version: "v1alpha1", Resource: "appdeployments",
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
			{"spec", "port"},
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
			name: "rejects a missing port",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Port = 0
			},
		},
		{
			name: "rejects a port above 65535",
			mutate: func(appDeployment *platformv1alpha1.AppDeployment) {
				appDeployment.Spec.Port = 65536
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

	t.Run("rejects an unknown field in strict mode", func(t *testing.T) {
		dynamicClient, err := dynamic.NewForConfig(testConfig)
		if err != nil {
			t.Fatalf("create dynamic client: %v", err)
		}
		resource := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "platform.fruto.calouro.tech/v1alpha1",
			"kind":       "AppDeployment",
			"metadata": map[string]any{
				"name":      "ap-unknownfield",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"workload": map[string]any{"kind": "Stateless", "stateless": map[string]any{}},
				"image":    testImage,
				"port":     int64(8080),
				"resources": map[string]any{
					"requests": map[string]any{"cpuMillis": int64(50), "memoryMiB": int64(64)},
					"limits":   map[string]any{"cpuMillis": int64(500), "memoryMiB": int64(256)},
				},
				"probes": map[string]any{
					"liveness":  map[string]any{"path": "/healthz"},
					"readiness": map[string]any{"path": "/readyz"},
				},
				"unexpected": true,
			},
		}}
		gvr := schema.GroupVersionResource{
			Group: "platform.fruto.calouro.tech", Version: "v1alpha1", Resource: "appdeployments",
		}
		_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, resource, metav1.CreateOptions{
			FieldValidation: metav1.FieldValidationStrict,
		})
		if err == nil {
			t.Fatal("expected strict field validation to reject the unknown field")
		}
	})

	t.Run("accepts and prunes an unknown field outside strict mode", func(t *testing.T) {
		dynamicClient, err := dynamic.NewForConfig(testConfig)
		if err != nil {
			t.Fatalf("create dynamic client: %v", err)
		}
		resource := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "platform.fruto.calouro.tech/v1alpha1",
			"kind":       "AppDeployment",
			"metadata": map[string]any{
				"name":      "ap-unknownfieldnonstrict",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"workload": map[string]any{"kind": "Stateless", "stateless": map[string]any{}},
				"image":    testImage,
				"port":     int64(8080),
				"resources": map[string]any{
					"requests": map[string]any{"cpuMillis": int64(50), "memoryMiB": int64(64)},
					"limits":   map[string]any{"cpuMillis": int64(500), "memoryMiB": int64(256)},
				},
				"probes": map[string]any{
					"liveness":  map[string]any{"path": "/healthz"},
					"readiness": map[string]any{"path": "/readyz"},
				},
				"unexpected": true,
			},
		}}
		gvr := schema.GroupVersionResource{
			Group: "platform.fruto.calouro.tech", Version: "v1alpha1", Resource: "appdeployments",
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

func TestReconcileCreatesPrivateService(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "private-service")
	appDeployment := newAppDeployment(namespace, "ap-privateservice", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile AppDeployment: %v", err)
	}

	service := &corev1.Service{}
	if err := testClient.Get(ctx, request.NamespacedName, service); err != nil {
		t.Fatalf("get private Service: %v", err)
	}
	if !metav1.IsControlledBy(service, appDeployment) {
		t.Fatal("expected Service to be controlled by AppDeployment")
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("expected ClusterIP Service, got %s", service.Spec.Type)
	}
}

func TestReconcileCorrectsPrivateServiceDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-drift")
	appDeployment := newAppDeployment(namespace, "ap-servicedrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	service := getService(t, ctx, request.NamespacedName)
	service.Spec.Type = corev1.ServiceTypeLoadBalancer
	service.Spec.Selector = map[string]string{"drift": "true"}
	service.Spec.ExternalIPs = []string{"192.0.2.10"}
	service.Spec.Ports = []corev1.ServicePort{{
		Name:     "drift",
		Protocol: corev1.ProtocolTCP,
		Port:     9999,
	}}
	if err := testClient.Update(ctx, service); err != nil {
		t.Fatalf("introduce Service drift: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile Service drift: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	assertPrivateService(t, service, appDeployment)
}

func TestReconcileRecoversServiceWithLoadBalancerClassDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-load-balancer-class-drift")
	appDeployment := newAppDeployment(namespace, "ap-lbclassdrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	service := getService(t, ctx, request.NamespacedName)
	loadBalancerClass := "example.test/load-balancer"
	service.Spec.Type = corev1.ServiceTypeLoadBalancer
	service.Spec.LoadBalancerClass = &loadBalancerClass
	if err := testClient.Update(ctx, service); err != nil {
		t.Fatalf("introduce LoadBalancerClass drift: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.Spec.Type != corev1.ServiceTypeLoadBalancer || service.Spec.LoadBalancerClass == nil ||
		*service.Spec.LoadBalancerClass != loadBalancerClass {
		t.Fatalf("expected LoadBalancerClass drift precondition, got type=%s loadBalancerClass=%v",
			service.Spec.Type, service.Spec.LoadBalancerClass)
	}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile LoadBalancerClass drift: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected LoadBalancerClass drift to converge without delayed retry, got %s", result.RequeueAfter)
	}

	service = getService(t, ctx, request.NamespacedName)
	if service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.LoadBalancerClass != nil {
		t.Errorf("expected private Service after reconciliation, got type=%s loadBalancerClass=%v",
			service.Spec.Type, service.Spec.LoadBalancerClass)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
	if degraded != nil && degraded.Status == metav1.ConditionTrue {
		t.Errorf("expected LoadBalancerClass drift recovery instead of Degraded=True, got reason=%s", degraded.Reason)
	}
}

func TestReconcileCorrectsServiceBehavioralDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-behavioral-drift")
	appDeployment := newAppDeployment(namespace, "ap-servicebehavior", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	service := getService(t, ctx, request.NamespacedName)
	service.Spec.Type = corev1.ServiceTypeLoadBalancer
	service.Spec.PublishNotReadyAddresses = true
	service.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	service.Spec.SessionAffinityConfig = &corev1.SessionAffinityConfig{
		ClientIP: &corev1.ClientIPConfig{TimeoutSeconds: pointerTo(int32(3600))},
	}
	service.Spec.InternalTrafficPolicy = pointerTo(corev1.ServiceInternalTrafficPolicyLocal)
	service.Spec.TrafficDistribution = pointerTo(corev1.ServiceTrafficDistributionPreferSameZone)
	service.Spec.LoadBalancerSourceRanges = []string{"192.0.2.0/24"}
	if err := testClient.Update(ctx, service); err != nil {
		t.Fatalf("introduce Service behavioral drift: %v", err)
	}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile Service behavioral drift: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected Service behavioral drift to converge without delayed retry, got %s",
			result.RequeueAfter)
	}
	service = getService(t, ctx, request.NamespacedName)
	resourceVersion := service.ResourceVersion
	if service.Spec.PublishNotReadyAddresses {
		t.Error("expected publishNotReadyAddresses drift to be cleared")
	}
	if service.Spec.SessionAffinity != corev1.ServiceAffinityNone || service.Spec.SessionAffinityConfig != nil {
		t.Errorf("expected session affinity drift to be cleared, got affinity=%s config=%#v",
			service.Spec.SessionAffinity, service.Spec.SessionAffinityConfig)
	}
	if service.Spec.InternalTrafficPolicy != nil &&
		*service.Spec.InternalTrafficPolicy == corev1.ServiceInternalTrafficPolicyLocal {
		t.Error("expected Local internal traffic policy drift to be cleared")
	}
	if service.Spec.TrafficDistribution != nil {
		t.Errorf("expected traffic distribution drift to be cleared, got %q", *service.Spec.TrafficDistribution)
	}
	if len(service.Spec.LoadBalancerSourceRanges) != 0 {
		t.Errorf("expected load balancer source ranges drift to be cleared, got %v",
			service.Spec.LoadBalancerSourceRanges)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
	if degraded != nil && degraded.Status == metav1.ConditionTrue {
		t.Errorf("expected Service drift recovery instead of Degraded=True, got reason=%s", degraded.Reason)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile corrected Service: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.ResourceVersion != resourceVersion {
		t.Errorf("expected corrected Service to remain stable, got resourceVersions %s then %s",
			resourceVersion, service.ResourceVersion)
	}
}

func TestReconcileCorrectsSecuritySensitivePodSpecDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "pod-security-drift")
	appDeployment := newAppDeployment(namespace, "ap-podsecuritydrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	deployment := getDeployment(t, ctx, request.NamespacedName)
	deployment.Spec.Template.Spec.HostNetwork = true
	deployment.Spec.Template.Spec.HostPID = true
	deployment.Spec.Template.Spec.HostIPC = true
	deployment.Spec.Template.Spec.NodeName = "drifted-node"
	deployment.Spec.Template.Spec.SchedulerName = "drifted-scheduler"
	deployment.Spec.Template.Spec.ReadinessGates = []corev1.PodReadinessGate{{
		ConditionType: "platform.example.test/ready",
	}}
	deployment.Spec.Template.Spec.RuntimeClassName = pointerTo("drifted-runtime")
	deployment.Spec.Template.Spec.SchedulingGates = []corev1.PodSchedulingGate{{Name: "drifted-gate"}}
	deployment.Spec.Template.Spec.ServiceAccountName = "drifted-service-account"
	deployment.Spec.Template.Spec.AutomountServiceAccountToken = pointerTo(true)
	deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{
		Name:  "privileged-init",
		Image: testImage,
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointerTo(true),
		},
	}}
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{{
		Name: "host-root",
		VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
			Path: "/",
		}},
	}}
	if err := testClient.Update(ctx, deployment); err != nil {
		t.Fatalf("introduce security-sensitive PodSpec drift: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile security-sensitive PodSpec drift: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	resourceVersion := deployment.ResourceVersion
	podSpec := deployment.Spec.Template.Spec
	if podSpec.HostNetwork || podSpec.HostPID || podSpec.HostIPC {
		t.Errorf("expected host namespace drift to be cleared, got hostNetwork=%t hostPID=%t hostIPC=%t",
			podSpec.HostNetwork, podSpec.HostPID, podSpec.HostIPC)
	}
	if podSpec.NodeName != "" || podSpec.SchedulerName != corev1.DefaultSchedulerName ||
		len(podSpec.ReadinessGates) != 0 ||
		podSpec.RuntimeClassName != nil || len(podSpec.SchedulingGates) != 0 {
		t.Errorf("expected execution-control drift to be cleared, got %#v", podSpec)
	}
	if len(podSpec.InitContainers) != 0 {
		t.Errorf("expected injected init containers to be removed, got %#v", podSpec.InitContainers)
	}
	if len(podSpec.Volumes) != 0 {
		t.Errorf("expected injected volumes to be removed, got %#v", podSpec.Volumes)
	}
	if podSpec.ServiceAccountName != "" {
		t.Errorf("expected drifted service account to be cleared, got %q", podSpec.ServiceAccountName)
	}
	if podSpec.AutomountServiceAccountToken == nil || *podSpec.AutomountServiceAccountToken {
		t.Errorf("expected service account token automount to be explicitly disabled, got %v",
			podSpec.AutomountServiceAccountToken)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile corrected PodSpec: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.ResourceVersion != resourceVersion {
		t.Errorf("expected corrected Deployment to remain stable, got resourceVersions %s then %s",
			resourceVersion, deployment.ResourceVersion)
	}
}

func TestReconcileCorrectsDeploymentRolloutDrift(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "deployment-rollout-drift")
	appDeployment := newAppDeployment(namespace, "ap-rolloutdrift", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	deployment := getDeployment(t, ctx, request.NamespacedName)
	deployment.Spec.Paused = true
	deployment.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	deployment.Spec.MinReadySeconds = 60
	deployment.Spec.ProgressDeadlineSeconds = pointerTo(int32(1200))
	deployment.Spec.RevisionHistoryLimit = pointerTo(int32(1))
	if err := testClient.Update(ctx, deployment); err != nil {
		t.Fatalf("introduce Deployment rollout drift: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile Deployment rollout drift: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	resourceVersion := deployment.ResourceVersion
	if deployment.Spec.Paused {
		t.Error("expected paused drift to be cleared")
	}
	strategy := deployment.Spec.Strategy
	if strategy.Type != appsv1.RollingUpdateDeploymentStrategyType || strategy.RollingUpdate == nil ||
		strategy.RollingUpdate.MaxUnavailable == nil || strategy.RollingUpdate.MaxUnavailable.String() != "25%" ||
		strategy.RollingUpdate.MaxSurge == nil || strategy.RollingUpdate.MaxSurge.String() != "25%" {
		t.Errorf("expected the default RollingUpdate strategy, got %#v", strategy)
	}
	if deployment.Spec.MinReadySeconds != 0 {
		t.Errorf("expected minReadySeconds drift to be cleared, got %d", deployment.Spec.MinReadySeconds)
	}
	if deployment.Spec.ProgressDeadlineSeconds == nil || *deployment.Spec.ProgressDeadlineSeconds != 600 {
		t.Errorf("expected the default 600 second progress deadline, got %v",
			deployment.Spec.ProgressDeadlineSeconds)
	}
	if deployment.Spec.RevisionHistoryLimit == nil || *deployment.Spec.RevisionHistoryLimit != 10 {
		t.Errorf("expected the default revision history limit, got %v", deployment.Spec.RevisionHistoryLimit)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile corrected Deployment rollout: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.ResourceVersion != resourceVersion {
		t.Errorf("expected corrected Deployment rollout to remain stable, got resourceVersions %s then %s",
			resourceVersion, deployment.ResourceVersion)
	}
}

func TestServiceOwnershipConflictPreservesObservedGeneration(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "service-ownership")
	appDeployment := newAppDeployment(namespace, "ap-serviceownership", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}
	unowned := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"test": "unowned"},
			Ports: []corev1.ServicePort{{
				Name:     "http",
				Protocol: corev1.ProtocolTCP,
				Port:     8080,
			}},
		},
	}
	if err := testClient.Create(ctx, unowned); err != nil {
		t.Fatalf("create unowned Service: %v", err)
	}

	recorder := record.NewFakeRecorder(5)
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme, Recorder: recorder}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected Service ownership conflict in status, got %v", err)
	}
	if result.RequeueAfter != ownershipConflictRequeueAfter {
		t.Fatalf("expected ownership conflict requeue after %s, got %s",
			ownershipConflictRequeueAfter, result.RequeueAfter)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	if stored.Status.ObservedGeneration != 0 || stored.Status.ObservedRelease != "" || stored.Status.WorkloadRef != nil {
		t.Fatalf("expected Service conflict to preserve observed fields, got %#v", stored.Status)
	}
	assertCondition(t, stored, platformv1alpha1.ConditionDegraded, metav1.ConditionTrue,
		platformv1alpha1.ReasonOwnershipConflict)
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "DeploymentCreated") {
			t.Fatalf("expected the successful Deployment transition before the Service conflict, got %q", event)
		}
	default:
		t.Fatal("expected the successful Deployment transition to be recorded")
	}
}

func TestReconcileCreatesUpdatesAndRecoversDeployment(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "reconcile")
	appDeployment := newAppDeployment(namespace, "ap-reconcile0001", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	deployment := getDeployment(t, ctx, request.NamespacedName)
	if !metav1.IsControlledBy(deployment, appDeployment) {
		t.Fatal("expected Deployment to be controlled by AppDeployment")
	}
	if got := deployment.Spec.Template.Spec.Containers[0].Image; got != testImage {
		t.Fatalf("expected image %q, got %q", testImage, got)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("expected one replica, got %v", deployment.Spec.Replicas)
	}
	assertDeploymentRuntime(t, deployment, appDeployment.Spec)

	service := getService(t, ctx, request.NamespacedName)
	assertPrivateService(t, service, appDeployment)

	stored := getAppDeployment(t, ctx, request.NamespacedName)
	assertCondition(t, stored, platformv1alpha1.ConditionProgressing, metav1.ConditionTrue,
		platformv1alpha1.ReasonDeploymentProgressing)
	assertCondition(t, stored, platformv1alpha1.ConditionReady, metav1.ConditionFalse,
		platformv1alpha1.ReasonDeploymentProgressing)
	if stored.Status.ObservedGeneration != stored.Generation {
		t.Fatalf("expected observed generation %d, got %d", stored.Generation, stored.Status.ObservedGeneration)
	}

	deploymentResourceVersion := deployment.ResourceVersion
	serviceResourceVersion := service.ResourceVersion
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.ResourceVersion != deploymentResourceVersion {
		t.Fatalf("expected idempotent reconcile to preserve Deployment resourceVersion, got %s then %s",
			deploymentResourceVersion, deployment.ResourceVersion)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.ResourceVersion != serviceResourceVersion {
		t.Fatalf("expected idempotent reconcile to preserve Service resourceVersion, got %s then %s",
			serviceResourceVersion, service.ResourceVersion)
	}

	stored.Spec.Image = otherImage
	stored.Spec.Replicas = pointerTo(int32(2))
	stored.Spec.Port = 9090
	stored.Spec.Resources.Requests = platformv1alpha1.AppDeploymentResourceValues{
		CPUMillis: 75,
		MemoryMiB: 96,
	}
	stored.Spec.Resources.Limits = platformv1alpha1.AppDeploymentResourceValues{
		CPUMillis: 750,
		MemoryMiB: 384,
	}
	stored.Spec.Probes.Liveness.Path = "/live"
	stored.Spec.Probes.Readiness.Path = "/ready"
	if err := testClient.Update(ctx, stored); err != nil {
		t.Fatalf("update AppDeployment: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile updated AppDeployment: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.Spec.Template.Spec.Containers[0].Image != otherImage {
		t.Fatalf("expected updated image %q, got %q", otherImage, deployment.Spec.Template.Spec.Containers[0].Image)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Fatalf("expected two replicas, got %v", deployment.Spec.Replicas)
	}
	assertDeploymentRuntime(t, deployment, stored.Spec)
	service = getService(t, ctx, request.NamespacedName)
	assertPrivateService(t, service, stored)
	if service.Spec.Ports[0].Port != 9090 {
		t.Fatalf("expected updated Service port 9090, got %d", service.Spec.Ports[0].Port)
	}

	previousUID := deployment.UID
	if err := testClient.Delete(ctx, deployment); err != nil {
		t.Fatalf("delete managed Deployment: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile deleted child: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.UID == previousUID {
		t.Fatal("expected deleted Deployment to be recreated with a new UID")
	}

	previousServiceUID := service.UID
	if err := testClient.Delete(ctx, service); err != nil {
		t.Fatalf("delete managed Service: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile deleted Service: %v", err)
	}
	service = getService(t, ctx, request.NamespacedName)
	if service.UID == previousServiceUID {
		t.Fatal("expected deleted Service to be recreated with a new UID")
	}
	assertPrivateService(t, service, stored)

	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 2
	deployment.Status.UpdatedReplicas = 2
	deployment.Status.ReadyReplicas = 2
	deployment.Status.AvailableReplicas = 2
	if err := testClient.Status().Update(ctx, deployment); err != nil {
		t.Fatalf("mark Deployment available: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile available Deployment: %v", err)
	}
	stored = getAppDeployment(t, ctx, request.NamespacedName)
	assertCondition(t, stored, platformv1alpha1.ConditionReady, metav1.ConditionTrue,
		platformv1alpha1.ReasonDeploymentAvailable)
	assertCondition(t, stored, platformv1alpha1.ConditionProgressing, metav1.ConditionFalse,
		platformv1alpha1.ReasonDeploymentAvailable)
	assertCondition(t, stored, platformv1alpha1.ConditionDegraded, metav1.ConditionFalse,
		platformv1alpha1.ReasonDeploymentAvailable)
	if stored.Status.ObservedRelease != otherImage {
		t.Fatalf("expected observed release %q, got %q", otherImage, stored.Status.ObservedRelease)
	}
	if stored.Status.WorkloadRef == nil || stored.Status.WorkloadRef.Name != deployment.Name {
		t.Fatalf("expected workload reference %q, got %#v", deployment.Name, stored.Status.WorkloadRef)
	}
}

func TestOwnershipConflictPreservesObservedGeneration(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "ownership")
	appDeployment := newAppDeployment(namespace, "ap-ownership0001", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	stored := getAppDeployment(t, ctx, request.NamespacedName)
	previousObservedGeneration := stored.Status.ObservedGeneration

	managedDeployment := getDeployment(t, ctx, request.NamespacedName)
	if err := testClient.Delete(ctx, managedDeployment); err != nil {
		t.Fatalf("delete managed Deployment: %v", err)
	}
	stored.Spec.Image = otherImage
	if err := testClient.Update(ctx, stored); err != nil {
		t.Fatalf("update AppDeployment: %v", err)
	}

	unowned := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointerTo(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"test": "unowned"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test": "unowned"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: testImage}}},
			},
		},
	}
	if err := testClient.Create(ctx, unowned); err != nil {
		t.Fatalf("create unowned Deployment: %v", err)
	}

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("expected ownership conflict to be reported as status, got %v", err)
	}
	if result.RequeueAfter != ownershipConflictRequeueAfter {
		t.Fatalf("expected ownership conflict requeue after %s, got %s",
			ownershipConflictRequeueAfter, result.RequeueAfter)
	}
	stored = getAppDeployment(t, ctx, request.NamespacedName)
	if stored.Status.ObservedGeneration != previousObservedGeneration {
		t.Fatalf("expected observed generation to remain %d, got %d",
			previousObservedGeneration, stored.Status.ObservedGeneration)
	}
	assertCondition(t, stored, platformv1alpha1.ConditionDegraded, metav1.ConditionTrue,
		platformv1alpha1.ReasonOwnershipConflict)
	if strings.Contains(meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded).Message, "uid") {
		t.Fatal("expected degraded message to remain sanitized")
	}
}

func TestReconcileSkipsTerminatingAppDeployment(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "terminating")
	appDeployment := newAppDeployment(namespace, "ap-terminating0001", testImage)
	appDeployment.Finalizers = []string{"test.fruto.calouro.tech/hold"}
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
			Port:         8080,
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
				Liveness:  platformv1alpha1.AppDeploymentHTTPProbe{Path: "/healthz"},
				Readiness: platformv1alpha1.AppDeploymentHTTPProbe{Path: "/readyz"},
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
		container.Ports[0].ContainerPort != spec.Port || container.Ports[0].Protocol != corev1.ProtocolTCP {
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
	assertHTTPProbe(t, "startup", container.StartupProbe, spec.Probes.Readiness.Path, 2, 30)
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
		service.Spec.Ports[0].Port != appDeployment.Spec.Port ||
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
