package controller

import (
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gateway "sigs.k8s.io/gateway-api/apis/v1"
)

func TestTerminalWithdrawalWaitsForOwnedChildrenAndPreservesForeignRoutes(t *testing.T) {
	ctx := t.Context()
	namespace := createTestNamespace(t, "withdrawal")
	app := newAppDeployment(namespace, "ap-terminal", testImage)
	app.Spec.Withdrawn = true
	if err := testClient.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	owned := &gateway.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "owned", Finalizers: []string{"test.molejo.dev/hold"}}}
	if err := ctrl.SetControllerReference(app, owned, testScheme); err != nil {
		t.Fatal(err)
	}
	if err := testClient.Create(ctx, owned); err != nil {
		t.Fatal(err)
	}
	foreign := &gateway.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "foreign"}}
	if err := testClient.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	key := client.ObjectKeyFromObject(app)
	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	if err != nil || result.RequeueAfter == 0 {
		t.Fatalf("removal pending: %+v %v", result, err)
	}
	current := getAppDeployment(t, ctx, key)
	if meta.IsStatusConditionTrue(current.Status.Conditions, "Withdrawn") {
		t.Fatal("confirmed before finalizer completed")
	}
	if err = testClient.Get(ctx, client.ObjectKeyFromObject(foreign), foreign); err != nil || !foreign.DeletionTimestamp.IsZero() {
		t.Fatal("foreign route was changed")
	}
	if err = testClient.Get(ctx, client.ObjectKeyFromObject(owned), owned); err != nil {
		t.Fatal(err)
	}
	if owned.DeletionTimestamp.IsZero() {
		t.Fatal("owned route removal was not requested")
	}
	// envtest has no garbage collector; complete the held child deletion explicitly.
	owned.Finalizers = nil
	if err = testClient.Update(ctx, owned); err != nil {
		t.Fatal(err)
	}
	if err = testClient.Get(ctx, client.ObjectKeyFromObject(owned), owned); !apierrors.IsNotFound(err) {
		t.Fatalf("child still exists: %v", err)
	}
	result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("confirmation: %+v %v", result, err)
	}
	current = getAppDeployment(t, ctx, key)
	if !current.Spec.Withdrawn || !meta.IsStatusConditionTrue(current.Status.Conditions, "Withdrawn") {
		t.Fatal("terminal barrier or correlated confirmation missing")
	}
}
