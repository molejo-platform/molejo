package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	list := &platformv1alpha1.AppDeploymentList{}
	if err := kubernetesClient.client.List(context.Background(), list, client.InNamespace("fruto-workspaces")); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected one AppDeployment for the stable runtime name, got %d", len(list.Items))
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
		Name:     name,
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
