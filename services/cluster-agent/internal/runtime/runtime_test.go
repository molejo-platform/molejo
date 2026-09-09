package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
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
	if observation.State == runtimecontract.StateReady {
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

	if got := observation(obj, image); got.State == runtimecontract.StateReady {
		t.Fatalf("stale observed release was reported as Ready: %+v", got)
	}
}

func TestRuntimeObservationsIncludeOnlyControlPlaneOwnedObjects(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	owned := &platformv1alpha1.AppDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "ap-owned", Namespace: "workspace-one", Generation: 2, Annotations: map[string]string{controlPlaneOwnerAnnotation: "ap-owned", desiredVersionAnnotation: "3"}},
		Spec:       platformv1alpha1.AppDeploymentSpec{Image: "registry.example/app@sha256:" + strings.Repeat("a", 64)},
		Status:     platformv1alpha1.AppDeploymentStatus{ObservedGeneration: 2, ObservedRelease: "registry.example/app@sha256:" + strings.Repeat("a", 64)},
	}
	unowned := owned.DeepCopy()
	unowned.Name = "ap-unowned"
	unowned.Annotations = nil
	volume := &platformv1alpha1.AppVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "vol-owned", Namespace: "workspace-one", Annotations: map[string]string{controlPlaneOwnerAnnotation: "vol-owned"}},
		Status:     platformv1alpha1.AppVolumeStatus{State: platformv1alpha1.VolumeStateReady, ObservedGeneration: 1, ObservedSizeGiB: 2},
	}
	kubernetesClient := &KubernetesClient{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(owned, unowned, volume).Build(), applyTimeout: time.Second}

	observations, err := kubernetesClient.RuntimeObservations(t.Context())
	if err != nil || len(observations) != 2 {
		t.Fatalf("observations=%+v err=%v", observations, err)
	}
	if observations[0].GetName() != "ap-owned" || observations[0].GetDesiredVersion() != 3 || len(observations[0].GetSpecHash()) != 64 || observations[1].GetName() != "vol-owned" || len(observations[1].GetSpecHash()) != 64 {
		t.Fatalf("unexpected observations: %+v", observations)
	}
}

func TestRuntimeObservationsApplyLimitAfterOwnershipFiltering(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	objects := make([]client.Object, 0, 1002)
	for index := range 1001 {
		objects = append(objects, &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("external-%04d", index), Namespace: "external"}})
	}
	objects = append(objects, &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: "ap-owned", Namespace: "workspace-one", Annotations: map[string]string{controlPlaneOwnerAnnotation: "ap-owned"}}})
	kubernetesClient := &KubernetesClient{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(), applyTimeout: time.Second}

	observations, err := kubernetesClient.RuntimeObservations(t.Context())
	if err != nil || len(observations) != 1 || observations[0].GetName() != "ap-owned" {
		t.Fatalf("observations=%d err=%v", len(observations), err)
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
	second.Image = "ghcr.io/molejo-platform/testkit@sha256:" + strings.Repeat("b", 64)
	second.Replicas = 3
	second.Port = 9090
	second.Resources.Requests.CPUMillis = 125
	second.Resources.Requests.MemoryMiB = 192
	second.Resources.Limits.CPUMillis = 500
	second.Resources.Limits.MemoryMiB = 384
	second.Probes.Liveness.Path = "/live"
	second.Probes.Readiness.Path = "/ready"
	second.Exposure = runtimecontract.ExposurePublic
	second.Slug = "demo-public"
	second.Variables = []runtimecontract.Variable{{Name: "APP_MODE", Value: "production"}}

	if err := kubernetesClient.ApplyDeployment(context.Background(), "molejo-workspaces", "ap-deployment-id", 1, first); err != nil {
		t.Fatal(err)
	}
	if err := kubernetesClient.ApplyDeployment(context.Background(), "molejo-workspaces", "ap-deployment-id", 2, second); err != nil {
		t.Fatal(err)
	}

	current := &platformv1alpha1.AppDeployment{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: "molejo-workspaces", Name: "ap-deployment-id"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Spec.Image != second.Image {
		t.Fatalf("expected the stable runtime to contain the latest image %q, got %q", second.Image, current.Spec.Image)
	}
	if current.Annotations[desiredVersionAnnotation] != "2" {
		t.Fatalf("desired version annotation=%q, want 2", current.Annotations[desiredVersionAnnotation])
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
	originalHash := objectSpecHash(current.Spec)
	(*current.Spec.Replicas)++
	if objectSpecHash(current.Spec) == originalHash {
		t.Fatal("replica drift did not change the canonical runtime spec hash")
	}
	list := &platformv1alpha1.AppDeploymentList{}
	if err := kubernetesClient.client.List(context.Background(), list, client.InNamespace("molejo-workspaces")); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected one AppDeployment for the stable runtime name, got %d", len(list.Items))
	}
}

func TestApplyVolumeChangesOnlyThePrivateInstallationBinding(t *testing.T) {
	for _, binding := range []string{"molejo-app-local", "ebs-csi-binding", "do-block-storage-binding"} {
		t.Run(binding, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := platformv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			kubernetesClient := &KubernetesClient{client: fake.NewClientBuilder().WithScheme(scheme).Build(), fieldManager: "test-control-plane", applyTimeout: time.Second}
			name := "vol-portability01"
			intent := VolumeIntent{RuntimeBinding: binding, SizeGiB: 2, RetentionPolicy: "Preserve", DesiredState: runtimecontract.VolumeDesiredReady}
			if err := kubernetesClient.ApplyVolume(context.Background(), "molejo-workspaces", name, 1, intent); err != nil {
				t.Fatal(err)
			}
			current := &platformv1alpha1.AppVolume{}
			if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: "molejo-workspaces", Name: name}, current); err != nil {
				t.Fatal(err)
			}
			if current.Spec.StorageClassName != binding || current.Spec.SizeGiB != intent.SizeGiB || current.Spec.RetentionPolicy != platformv1alpha1.VolumeRetentionPreserve {
				t.Fatalf("projected volume = %+v", current.Spec)
			}
		})
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
	namespace := "molejo-workspaces"
	managed := func(name, version string) metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":              workspaceOwnerValue,
				"platform.molejo.dev/configuration-version": version,
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
		client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, underlying client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, forbidden := list.(*corev1.SecretList); forbidden {
					return errors.New("listing Secrets is forbidden")
				}
				return underlying.List(ctx, list, opts...)
			},
		}).Build(),
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
	namespace := "molejo-workspaces"
	metadata := metav1.ObjectMeta{
		Name:      owner + "-c1",
		Namespace: namespace,
		Labels: map[string]string{
			"app.kubernetes.io/managed-by":              workspaceOwnerValue,
			"platform.molejo.dev/configuration-version": "1",
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

func TestEnsureWorkspacePlacementCreatesAndReusesTheTypedIntent(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	intent := runtimecontract.WorkspacePlacementIntent{WorkspaceID: "ws-abcdefghijklmnopqrst", NamespaceName: "ws-abcdefghijklmnopqrst", AccessProfile: "NamespacedRuntime", LifecycleState: "Ready"}
	for range 2 {
		if _, err := kubernetesClient.EnsureWorkspacePlacement(context.Background(), intent); err != nil {
			t.Fatal(err)
		}
	}

	current := &platformv1alpha1.WorkspacePlacement{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Name: intent.WorkspaceID}, current); err != nil {
		t.Fatal(err)
	}
	if current.Annotations[controlPlaneOwnerAnnotation] != workspaceOwnerValue {
		t.Fatalf("placement owner marker = %q", current.Annotations[controlPlaneOwnerAnnotation])
	}
}

func TestEnsureWorkspacePlacementRejectsAConflictingIntent(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &platformv1alpha1.WorkspacePlacement{ObjectMeta: metav1.ObjectMeta{Name: "ws-abcdefghijklmnopqrst"}, Spec: platformv1alpha1.WorkspacePlacementSpec{WorkspaceID: "ws-abcdefghijklmnopqrst", NamespaceName: "foreign"}}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	_, err := kubernetesClient.EnsureWorkspacePlacement(context.Background(), runtimecontract.WorkspacePlacementIntent{WorkspaceID: "ws-abcdefghijklmnopqrst", NamespaceName: "ws-abcdefghijklmnopqrst", AccessProfile: "NamespacedRuntime", LifecycleState: "Ready"})
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
}

func TestApplyDeploymentRejectsAnUnownedRootObject(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: "ap-deployment-id", Namespace: "molejo-workspaces"}}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	err := kubernetesClient.ApplyDeployment(context.Background(), "molejo-workspaces", "ap-deployment-id", 1, runtimeTestIntent("demo"))
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
}

func TestDeleteDeploymentRejectsAnUnownedRootObject(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: "ap-deployment-id", Namespace: "molejo-workspaces"}}
	kubernetesClient := &KubernetesClient{
		client:       fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build(),
		fieldManager: "test-control-plane",
		applyTimeout: time.Second,
	}

	err := kubernetesClient.DeleteDeployment(context.Background(), "molejo-workspaces", "ap-deployment-id")
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
	current := &platformv1alpha1.AppDeployment{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: "molejo-workspaces", Name: "ap-deployment-id"}, current); err != nil {
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

	err := kubernetesClient.ApplyDeployment(context.Background(), "molejo-workspaces", "ap-deployment-id", 1, runtimeTestIntent("demo"))
	if !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("expected ErrOwnershipConflict, got %v", err)
	}
	current := &platformv1alpha1.AppDeployment{}
	if err := kubernetesClient.client.Get(context.Background(), client.ObjectKey{Namespace: "molejo-workspaces", Name: "ap-deployment-id"}, current); err != nil {
		t.Fatal(err)
	}
	if current.Annotations[controlPlaneOwnerAnnotation] != "" {
		t.Fatal("control plane adopted the object that appeared during create")
	}
}

func runtimeTestIntent(name string) runtimecontract.DeploymentIntent {
	return runtimecontract.DeploymentIntent{
		Image:    "ghcr.io/molejo-platform/testkit@sha256:" + strings.Repeat("a", 64),
		Replicas: 1,
		Port:     8080,
		Resources: runtimecontract.Resources{
			Requests: runtimecontract.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   runtimecontract.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes: runtimecontract.Probes{
			Startup:   runtimecontract.Probe{Path: "/readyz"},
			Liveness:  runtimecontract.Probe{Path: "/healthz"},
			Readiness: runtimecontract.Probe{Path: "/readyz"},
		},
		Exposure: runtimecontract.ExposurePrivate,
	}
}
