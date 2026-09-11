package workspaceboundary

import (
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	kubemetadata "github.com/molejo-platform/molejo/packages/kubernetes-api/metadata"
)

const workspaceIDAnnotation = "platform.molejo.dev/workspace-id"

type Reconciler struct {
	client.Client
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).For(&platformv1alpha1.WorkspacePlacement{}).Named("workspace-boundary").Complete(r)
}

func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	placement := &platformv1alpha1.WorkspacePlacement{}
	if err := r.Get(ctx, request.NamespacedName, placement); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	plan, err := Plan(placement.Spec)
	if err != nil {
		return ctrl.Result{}, r.updateConditions(ctx, placement, false, "InvalidPlacement", err.Error())
	}
	if plan.Delete {
		return r.deleteBoundary(ctx, placement, plan)
	}
	if err = r.ensureNamespace(ctx, plan); err != nil {
		return ctrl.Result{}, r.updateConditions(ctx, placement, false, "NamespaceConflict", err.Error())
	}
	for _, binding := range plan.Bindings {
		if err = r.ensureBinding(ctx, plan, binding); err != nil {
			return ctrl.Result{}, r.updateConditions(ctx, placement, false, "AccessBindingFailed", err.Error())
		}
	}
	return ctrl.Result{}, r.updateConditions(ctx, placement, true, "BoundaryReady", "workspace namespace and fixed access bindings are ready")
}

func (r *Reconciler) ensureNamespace(ctx context.Context, plan ReconciliationPlan) error {
	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: plan.Namespace}, namespace)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: plan.Namespace, Labels: map[string]string{kubemetadata.ManagedByLabel: kubemetadata.ControlPlaneOwner, kubemetadata.PublicationNamespaceLabel: "enabled"}, Annotations: map[string]string{workspaceIDAnnotation: plan.WorkspaceID, kubemetadata.ControlPlaneOwnerAnnotation: kubemetadata.ControlPlaneOwner}}})
	}
	if err != nil {
		return err
	}
	if namespace.Annotations[workspaceIDAnnotation] != plan.WorkspaceID || namespace.Annotations[kubemetadata.ControlPlaneOwnerAnnotation] != kubemetadata.ControlPlaneOwner {
		return fmt.Errorf("namespace %s is not owned by Workspace %s", plan.Namespace, plan.WorkspaceID)
	}
	return nil
}

func (r *Reconciler) ensureBinding(ctx context.Context, plan ReconciliationPlan, binding Binding) error {
	key := types.NamespacedName{Namespace: plan.Namespace, Name: binding.Name}
	current := &rbacv1.RoleBinding{}
	err := r.Get(ctx, key, current)
	desired := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: binding.Name, Namespace: plan.Namespace, Labels: map[string]string{kubemetadata.ManagedByLabel: kubemetadata.ControlPlaneOwner}, Annotations: map[string]string{workspaceIDAnnotation: plan.WorkspaceID}}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: binding.RoleName}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: binding.ServiceAccountName, Namespace: binding.ServiceAccountNamespace}}}
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if current.Annotations[workspaceIDAnnotation] != plan.WorkspaceID || current.RoleRef != desired.RoleRef || !reflect.DeepEqual(current.Subjects, desired.Subjects) {
		return fmt.Errorf("RoleBinding %s/%s conflicts with the fixed access profile", plan.Namespace, binding.Name)
	}
	return nil
}

func (r *Reconciler) deleteBoundary(ctx context.Context, placement *platformv1alpha1.WorkspacePlacement, plan ReconciliationPlan) (ctrl.Result, error) {
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: plan.Namespace}, namespace); apierrors.IsNotFound(err) {
		return ctrl.Result{}, r.updateConditions(ctx, placement, true, "BoundaryDeleted", "workspace namespace is absent")
	} else if err != nil {
		return ctrl.Result{}, err
	}
	if namespace.Annotations[workspaceIDAnnotation] != plan.WorkspaceID || namespace.Annotations[kubemetadata.ControlPlaneOwnerAnnotation] != kubemetadata.ControlPlaneOwner {
		return ctrl.Result{}, r.updateConditions(ctx, placement, false, "NamespaceConflict", "foreign namespace will not be deleted")
	}
	if err := r.Delete(ctx, namespace); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func (r *Reconciler) updateConditions(ctx context.Context, placement *platformv1alpha1.WorkspacePlacement, ready bool, reason, message string) error {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	before := placement.DeepCopy()
	for _, conditionType := range []string{platformv1alpha1.WorkspacePlacementConditionNamespaceReady, platformv1alpha1.WorkspacePlacementConditionAgentAccessReady, platformv1alpha1.WorkspacePlacementConditionOperatorAccessReady, platformv1alpha1.WorkspacePlacementConditionPolicyReady} {
		apiMeta.SetStatusCondition(&placement.Status.Conditions, metav1.Condition{Type: conditionType, Status: status, ObservedGeneration: placement.Generation, Reason: reason, Message: message})
	}
	placement.Status.ObservedGeneration = placement.Generation
	if reflect.DeepEqual(before.Status, placement.Status) {
		return nil
	}
	return r.Status().Patch(ctx, placement, client.MergeFrom(before))
}
