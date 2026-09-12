package workspaceboundary

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/molejo-platform/molejo/packages/workspacecontract"
)

type transientRoleBindingClient struct {
	client.Client
	remainingFailures int
}

func (c *transientRoleBindingClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	if _, ok := object.(*rbacv1.RoleBinding); ok && c.remainingFailures > 0 {
		c.remainingFailures--
		return errors.New("transient RoleBinding write failure")
	}
	return c.Client.Create(ctx, object, options...)
}

func TestReconcileMaterializesOnlyTheWorkspaceBoundary(t *testing.T) {
	placement := testPlacement()
	kubernetesClient := boundaryTestClient(t, placement)
	reconciler := &Reconciler{Client: kubernetesClient}

	for range 2 {
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(placement)}); err != nil {
			t.Fatal(err)
		}
	}

	namespace := &corev1.Namespace{}
	if err := kubernetesClient.Get(context.Background(), client.ObjectKey{Name: placement.Spec.NamespaceName}, namespace); err != nil {
		t.Fatal(err)
	}
	if namespace.Annotations[workspaceIDAnnotation] != placement.Spec.WorkspaceID {
		t.Fatalf("workspace annotation=%q", namespace.Annotations[workspaceIDAnnotation])
	}

	bindings := &rbacv1.RoleBindingList{}
	if err := kubernetesClient.List(context.Background(), bindings, client.InNamespace(placement.Spec.NamespaceName)); err != nil {
		t.Fatal(err)
	}
	if len(bindings.Items) != 3 {
		t.Fatalf("RoleBindings=%d, want 3", len(bindings.Items))
	}

	current := &platformv1alpha1.WorkspacePlacement{}
	if err := kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(placement), current); err != nil {
		t.Fatal(err)
	}
	for _, conditionType := range []string{
		platformv1alpha1.WorkspacePlacementConditionNamespaceReady,
		platformv1alpha1.WorkspacePlacementConditionAgentAccessReady,
		platformv1alpha1.WorkspacePlacementConditionOperatorAccessReady,
		platformv1alpha1.WorkspacePlacementConditionPolicyReady,
	} {
		condition := apiMeta.FindStatusCondition(current.Status.Conditions, conditionType)
		if condition == nil || condition.Status != metav1.ConditionTrue || condition.ObservedGeneration != current.Generation {
			t.Fatalf("condition %s=%+v", conditionType, condition)
		}
	}
}

func TestReconcileNeverAdoptsAForeignNamespace(t *testing.T) {
	placement := testPlacement()
	foreign := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: placement.Spec.NamespaceName}}
	kubernetesClient := boundaryTestClient(t, placement, foreign)
	reconciler := &Reconciler{Client: kubernetesClient}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(placement)}); err != nil {
		t.Fatal(err)
	}

	bindings := &rbacv1.RoleBindingList{}
	if err := kubernetesClient.List(context.Background(), bindings, client.InNamespace(placement.Spec.NamespaceName)); err != nil {
		t.Fatal(err)
	}
	if len(bindings.Items) != 0 {
		t.Fatalf("foreign namespace received %d RoleBindings", len(bindings.Items))
	}
	current := &platformv1alpha1.WorkspacePlacement{}
	if err := kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(placement), current); err != nil {
		t.Fatal(err)
	}
	condition := apiMeta.FindStatusCondition(current.Status.Conditions, platformv1alpha1.WorkspacePlacementConditionPolicyReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "NamespaceConflict" {
		t.Fatalf("PolicyReady=%+v", condition)
	}
}

func TestReconcileRetriesTransientAccessBindingFailureUntilReady(t *testing.T) {
	placement := testPlacement()
	baseClient := boundaryTestClient(t, placement)
	kubernetesClient := &transientRoleBindingClient{Client: baseClient, remainingFailures: 2}
	reconciler := &Reconciler{Client: kubernetesClient}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(placement)}

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := reconciler.Reconcile(context.Background(), request); err == nil {
			t.Fatalf("attempt %d discarded a transient RoleBinding error", attempt+1)
		}
	}
	if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	current := &platformv1alpha1.WorkspacePlacement{}
	if err := baseClient.Get(context.Background(), client.ObjectKeyFromObject(placement), current); err != nil {
		t.Fatal(err)
	}
	condition := apiMeta.FindStatusCondition(current.Status.Conditions, platformv1alpha1.WorkspacePlacementConditionPolicyReady)
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "BoundaryReady" {
		t.Fatalf("PolicyReady=%+v", condition)
	}
}

func TestReconcileReobservesADeletedBoundaryBeforeReportingCompletion(t *testing.T) {
	placement := testPlacement()
	placement.Generation = 2
	placement.Spec.LifecycleState = workspacecontract.LifecycleDeleted
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: placement.Spec.NamespaceName,
		Annotations: map[string]string{
			workspaceIDAnnotation:                     placement.Spec.WorkspaceID,
			"platform.molejo.dev/control-plane-owner": "molejo-control-plane",
		},
	}}
	kubernetesClient := boundaryTestClient(t, placement, namespace)
	reconciler := &Reconciler{Client: kubernetesClient}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(placement)}

	result, err := reconciler.Reconcile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter <= 0 {
		t.Fatalf("deletion result=%+v, want a bounded reobservation", result)
	}
	if _, err = reconciler.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	current := &platformv1alpha1.WorkspacePlacement{}
	if err = kubernetesClient.Get(context.Background(), client.ObjectKeyFromObject(placement), current); err != nil {
		t.Fatal(err)
	}
	condition := apiMeta.FindStatusCondition(current.Status.Conditions, platformv1alpha1.WorkspacePlacementConditionPolicyReady)
	if current.Status.ObservedGeneration != current.Generation || condition == nil || condition.Reason != "BoundaryDeleted" || condition.Status != metav1.ConditionTrue {
		t.Fatalf("deleted placement status=%+v", current.Status)
	}
}

func boundaryTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&platformv1alpha1.WorkspacePlacement{}).WithObjects(objects...).Build()
}

func testPlacement() *platformv1alpha1.WorkspacePlacement {
	workspaceID := "ws-abcdefghijklmnopqrst"
	return &platformv1alpha1.WorkspacePlacement{
		ObjectMeta: metav1.ObjectMeta{Name: workspaceID, Generation: 1},
		Spec: platformv1alpha1.WorkspacePlacementSpec{
			WorkspaceID: workspaceID, NamespaceName: workspaceID,
			AccessProfile:  workspacecontract.AccessProfileNamespaced,
			LifecycleState: workspacecontract.LifecycleReady,
		},
	}
}
