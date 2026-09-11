package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

// The terminal AppDeployment is the write barrier, not an executable workload.
// A finalizer on any owned child keeps withdrawal pending and claims reserved.
func (r *AppDeploymentReconciler) reconcileWithdrawal(ctx context.Context, app *platformv1alpha1.AppDeployment) (ctrl.Result, error) {
	lists := []client.ObjectList{&gatewayv1.HTTPRouteList{}, &gatewayalpha.TCPRouteList{}, &appsv1.DeploymentList{}, &appsv1.StatefulSetList{}, &corev1.ServiceList{}}
	pending := false
	for _, list := range lists {
		if err := r.List(ctx, list, client.InNamespace(app.Namespace)); err != nil {
			if meta.IsNoMatchError(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		if err := meta.EachListItem(list, func(value runtime.Object) error {
			object := value.(client.Object)
			owner := metav1.GetControllerOf(object)
			if owner == nil || owner.UID != app.UID {
				return nil
			}
			pending = true
			if !object.GetDeletionTimestamp().IsZero() {
				return nil
			}
			uid, version := object.GetUID(), object.GetResourceVersion()
			err := r.Delete(ctx, object, client.Preconditions{UID: &uid, ResourceVersion: &version}, client.PropagationPolicy(metav1.DeletePropagationForeground))
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}); err != nil {
			return ctrl.Result{}, err
		}
	}
	status, reason := metav1.ConditionTrue, "ChildrenRemoved"
	if pending {
		status, reason = metav1.ConditionFalse, "ChildrenRemaining"
	}
	before := app.DeepCopy()
	meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{Type: "Withdrawn", Status: status, Reason: reason, ObservedGeneration: app.Generation, LastTransitionTime: metav1.Now()})
	app.Status.ObservedGeneration = app.Generation
	app.Status.EndpointStatuses = nil
	app.Status.WorkloadRef = nil
	app.Status.ObservedRelease = ""
	meta.RemoveStatusCondition(&app.Status.Conditions, "Ready")
	if err := r.Status().Patch(ctx, app, client.MergeFrom(before)); err != nil {
		return ctrl.Result{}, err
	}
	if pending {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{}, nil
}
