package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestObservationDoesNotReportReadyForAnUnobservedVersion(t *testing.T) {
	obj := &platformv1alpha1.AppDeployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Status: platformv1alpha1.AppDeploymentStatus{
			ObservedGeneration: 1,
			ObservedRelease:    "ghcr.io/example/demo@sha256:" + strings.Repeat("a", 64),
			Conditions: []metav1.Condition{
				{Type: platformv1alpha1.ConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: 1},
			},
		},
	}

	observation := observation(obj, obj.Status.ObservedRelease)
	if observation.State == domain.Ready {
		t.Fatalf("stale observed generation was reported as Ready: %+v", observation)
	}
}

func TestObservationDoesNotReportReadyForAStaleRelease(t *testing.T) {
	image := "ghcr.io/example/demo@sha256:" + strings.Repeat("a", 64)
	obj := &platformv1alpha1.AppDeployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Spec:       platformv1alpha1.AppDeploymentSpec{Image: image},
		Status: platformv1alpha1.AppDeploymentStatus{
			ObservedGeneration: 2,
			ObservedRelease:    "ghcr.io/example/demo@sha256:" + strings.Repeat("b", 64),
			Conditions: []metav1.Condition{
				{Type: platformv1alpha1.ConditionReady, Status: metav1.ConditionTrue, ObservedGeneration: 2},
			},
		},
	}

	if got := observation(obj, image); got.State == domain.Ready {
		t.Fatalf("stale observed release was reported as Ready: %+v", got)
	}
}

func TestApplyDeploymentKeepsTheRuntimeNameStableAcrossIntentUpdates(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}
	first := runtimeTestIntent("demo")
	second := runtimeTestIntent("demo")
	second.Image = "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("b", 64)
	second.Replicas = 3
	second.Port = 9090
	second.Resources.Requests.CPUMillis = 125
	second.Resources.Requests.MemoryMiB = 192
	second.Resources.Limits.CPUMillis = 500
	second.Resources.Limits.MemoryMiB = 384
	second.Probes.Liveness.Path = "/live"
	second.Probes.Readiness.Path = "/ready"
	second.Exposure = domain.ExposurePublic
	second.Slug = "demo-public"
	second.Variables = []domain.Variable{{Name: "APP_MODE", Value: "production"}}

	if err := kubernetesClient.ApplyDeployment(context.Background(), "fruto-workspaces", "ap-deployment-id", first); err != nil {
		t.Fatal(err)
	}
	if err := kubernetesClient.ApplyDeployment(context.Background(), "fruto-workspaces", "ap-deployment-id", second); err != nil {
		t.Fatal(err)
	}

	current := &platformv1alpha1.AppDeployment{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: "fruto-workspaces", Name: "ap-deployment-id"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Spec.Image != second.Image {
		t.Fatalf("expected the stable runtime to contain the latest image %q, got %q", second.Image, current.Spec.Image)
	}
	if current.Spec.Replicas == nil || *current.Spec.Replicas != second.Replicas || current.Spec.Port != second.Port {
		t.Fatalf("runtime scale/port projection does not match intent: %+v", current.Spec)
	}
	if current.Spec.Resources.Requests.CPUMillis != second.Resources.Requests.CPUMillis || current.Spec.Resources.Requests.MemoryMiB != second.Resources.Requests.MemoryMiB || current.Spec.Resources.Limits.CPUMillis != second.Resources.Limits.CPUMillis || current.Spec.Resources.Limits.MemoryMiB != second.Resources.Limits.MemoryMiB {
		t.Fatalf("runtime resource projection does not match intent: %+v", current.Spec.Resources)
	}
	if current.Spec.Probes.Liveness.Path != second.Probes.Liveness.Path || current.Spec.Probes.Readiness.Path != second.Probes.Readiness.Path {
		t.Fatalf("runtime probe projection does not match intent: %+v", current.Spec.Probes)
	}
	if current.Spec.Exposure != platformv1alpha1.ExposurePublic || current.Spec.Slug != second.Slug {
		t.Fatalf("runtime exposure projection does not match intent: exposure=%q slug=%q", current.Spec.Exposure, current.Spec.Slug)
	}
	if len(current.Spec.Variables) != 1 || current.Spec.Variables[0].Name != "APP_MODE" || current.Spec.Variables[0].Value != "production" {
		t.Fatalf("runtime variables do not match intent: %+v", current.Spec.Variables)
	}
	list := &platformv1alpha1.AppDeploymentList{}
	if err := kubernetesClient.client.List(context.Background(), list, client.InNamespace("fruto-workspaces")); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected one AppDeployment for the stable runtime name, got %d", len(list.Items))
	}
}

func TestGarbageCollectConfigurationKeepsOnlyCurrentOwnedObjects(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	owner := "ap-deployment-id"
	namespace := "fruto-workspaces"
	managed := func(name, version string) metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":                      workspaceOwnerValue,
				"platform.fruto.calouro.tech/configuration-version": version,
			},
			Annotations: map[string]string{controlPlaneOwnerAnnotation: owner},
		}
	}
	objects := []client.Object{
		&platformv1alpha1.AppDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: owner, Namespace: namespace, Annotations: map[string]string{controlPlaneOwnerAnnotation: owner}},
			Spec:       platformv1alpha1.AppDeploymentSpec{ConfigMapRef: owner + "-c2", SecretRef: owner + "-c2-secret"},
		},
		&corev1.ConfigMap{ObjectMeta: managed(owner+"-c1", "1")},
		&corev1.ConfigMap{ObjectMeta: managed(owner+"-c2", "2")},
		&corev1.Secret{ObjectMeta: managed(owner+"-c1-secret", "1")},
		&corev1.Secret{ObjectMeta: managed(owner+"-c2-secret", "2")},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: owner + "-c3", Namespace: namespace}},
	}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	if err := kubernetesClient.GarbageCollectConfiguration(context.Background(), namespace, owner); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{owner + "-c2", owner + "-c3"} {
		if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, &corev1.ConfigMap{}); err != nil {
			t.Fatalf("kept ConfigMap %s: %v", name, err)
		}
	}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: owner + "-c2-secret"}, &corev1.Secret{}); err != nil {
		t.Fatalf("kept Secret: %v", err)
	}
	for _, object := range []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: owner + "-c1", Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: owner + "-c1-secret", Namespace: namespace}},
	} {
		if err := kubernetesClient.client.Get(context.Background(), client.ObjectKeyFromObject(object), object); !apierrors.IsNotFound(err) {
			t.Fatalf("stale configuration %T/%s was not deleted: %v", object, object.GetName(), err)
		}
	}
}

func TestGarbageCollectConfigurationRemovesAllOwnedObjectsAfterRootDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	owner := "ap-deployment-id"
	namespace := "fruto-workspaces"
	metadata := metav1.ObjectMeta{
		Name:      owner + "-c1",
		Namespace: namespace,
		Labels: map[string]string{
			"app.kubernetes.io/managed-by":                      workspaceOwnerValue,
			"platform.fruto.calouro.tech/configuration-version": "1",
		},
		Annotations: map[string]string{controlPlaneOwnerAnnotation: owner},
	}
	kubernetesClient := &KubernetesClient{
		client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&corev1.ConfigMap{ObjectMeta: metadata},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: owner + "-c1-secret", Namespace: namespace, Labels: metadata.Labels, Annotations: metadata.Annotations,
			}},
		).Build(),
		applyTimeout: time.Second,
	}

	if err := kubernetesClient.GarbageCollectConfiguration(context.Background(), namespace, owner); err != nil {
		t.Fatal(err)
	}
	var configMaps corev1.ConfigMapList
	if err := kubernetesClient.client.List(context.Background(), &configMaps, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	var secrets corev1.SecretList
	if err := kubernetesClient.client.List(context.Background(), &secrets, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	if len(configMaps.Items) != 0 || len(secrets.Items) != 0 {
		t.Fatalf("configuration was not collected: ConfigMaps=%d Secrets=%d", len(configMaps.Items), len(secrets.Items))
	}
}

func TestEnsureWorkspaceCreatesAndReusesTheManagedNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	for range 2 {
		if err := kubernetesClient.EnsureWorkspace(context.Background(), "fruto-workspaces"); err != nil {
			t.Fatal(err)
		}
	}

	current := &corev1.Namespace{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Name: "fruto-workspaces"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Annotations[controlPlaneOwnerAnnotation] != workspaceOwnerValue {
		t.Fatalf("namespace owner marker = %q", current.Annotations[controlPlaneOwnerAnnotation])
	}
}

func TestEnsureWorkspaceRejectsAnUnmanagedNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fruto-workspaces"}}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	err := kubernetesClient.EnsureWorkspace(context.Background(), "fruto-workspaces")
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
}

func TestPreflightRejectsAnUnexpectedClusterUID(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	systemNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceSystem, UID: "actual-cluster-uid"}}
	kubernetesClient := &KubernetesClient{
		client:             fake.NewClientBuilder().WithScheme(scheme).WithObjects(systemNamespace).Build(),
		applyTimeout:       time.Second,
		expectedClusterUID: "different-cluster-uid",
	}

	if err := kubernetesClient.Preflight(context.Background()); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected cluster identity mismatch, got %v", err)
	}
	kubernetesClient.expectedClusterUID = "actual-cluster-uid"
	if err := kubernetesClient.Preflight(context.Background()); err != nil {
		t.Fatalf("matching cluster identity was rejected: %v", err)
	}
}

func TestExternalClientFailsClosedOnUnexpectedContextOrServer(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	raw := clientcmdapi.Config{
		CurrentContext: "actual-context",
		Contexts: map[string]*clientcmdapi.Context{
			"actual-context": {Cluster: "actual-cluster", AuthInfo: "actor"},
		},
		Clusters: map[string]*clientcmdapi.Cluster{
			"actual-cluster": {Server: "https://127.0.0.1:6443", InsecureSkipTLSVerify: true},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"actor": {Token: "test-only"}},
	}
	if err := clientcmd.WriteToFile(raw, kubeconfig); err != nil {
		t.Fatal(err)
	}

	base := ExternalConfig{Kubeconfig: kubeconfig, Context: "wrong-context", Server: "https://127.0.0.1:6443", ExpectedClusterUID: "expected-uid"}
	if _, err := NewKubernetesClient(base, "test", time.Second); err == nil || !strings.Contains(err.Error(), "current context") {
		t.Fatalf("unexpected context was not rejected: %v", err)
	}
	base.Context = "actual-context"
	base.Server = "https://127.0.0.1:7443"
	if _, err := NewKubernetesClient(base, "test", time.Second); err == nil || !strings.Contains(err.Error(), "expected server") {
		t.Fatalf("unexpected server was not rejected: %v", err)
	}
	base.Server = "https://127.0.0.1:6443/"
	if _, err := NewKubernetesClient(base, "test", time.Second); err != nil {
		t.Fatalf("exact external identity was rejected: %v", err)
	}
}

func TestApplyDeploymentRejectsAnUnownedRootObject(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: "ap-deployment-id", Namespace: "fruto-workspaces"}}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	err := kubernetesClient.ApplyDeployment(context.Background(), "fruto-workspaces", "ap-deployment-id", runtimeTestIntent("demo"))
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
}

func TestDeleteDeploymentRejectsAnUnownedRootObject(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: "ap-deployment-id", Namespace: "fruto-workspaces"}}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	err := kubernetesClient.DeleteDeployment(context.Background(), "fruto-workspaces", "ap-deployment-id")
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
	current := &platformv1alpha1.AppDeployment{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: "fruto-workspaces", Name: "ap-deployment-id"}, current); err != nil {
		t.Fatalf("unowned root object was deleted: %v", err)
	}
}

func TestApplyDeploymentRejectsAnObjectCreatedAfterTheOwnershipCheck(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	intercepted := false
	kubernetesClient := &KubernetesClient{
		client: fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, underlying client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if !intercepted {
					intercepted = true
					unowned := &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: obj.GetName(), Namespace: obj.GetNamespace()}}
					if err := underlying.Create(ctx, unowned); err != nil {
						return err
					}
				}
				return underlying.Create(ctx, obj, opts...)
			},
		}).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	err := kubernetesClient.ApplyDeployment(context.Background(), "fruto-workspaces", "ap-deployment-id", runtimeTestIntent("demo"))
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
	current := &platformv1alpha1.AppDeployment{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: "fruto-workspaces", Name: "ap-deployment-id"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Annotations[controlPlaneOwnerAnnotation] != "" {
		t.Fatal("control plane adopted the object that appeared during create")
	}
}

func runtimeTestIntent(name string) domain.Intent {
	return domain.Intent{
		Image:    "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("a", 64),
		Replicas: 1,
		Port:     8080,
		Resources: domain.Resources{
			Requests: domain.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   domain.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes: domain.Probes{
			Liveness:  domain.Probe{Path: "/healthz"},
			Readiness: domain.Probe{Path: "/readyz"},
		},
		Exposure: domain.ExposurePrivate,
	}
}
