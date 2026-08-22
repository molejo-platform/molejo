package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
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
				"image":      testImage,
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
				"image":      testImage,
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

	stored := getAppDeployment(t, ctx, request.NamespacedName)
	assertCondition(t, stored, platformv1alpha1.ConditionProgressing, metav1.ConditionTrue,
		platformv1alpha1.ReasonDeploymentProgressing)
	assertCondition(t, stored, platformv1alpha1.ConditionReady, metav1.ConditionFalse,
		platformv1alpha1.ReasonDeploymentProgressing)
	if stored.Status.ObservedGeneration != stored.Generation {
		t.Fatalf("expected observed generation %d, got %d", stored.Generation, stored.Status.ObservedGeneration)
	}

	resourceVersion := deployment.ResourceVersion
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	deployment = getDeployment(t, ctx, request.NamespacedName)
	if deployment.ResourceVersion != resourceVersion {
		t.Fatalf("expected idempotent reconcile to preserve Deployment resourceVersion, got %s then %s",
			resourceVersion, deployment.ResourceVersion)
	}

	stored.Spec.Image = otherImage
	stored.Spec.Replicas = pointerTo(int32(2))
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
		Spec:       platformv1alpha1.AppDeploymentSpec{Image: image},
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
