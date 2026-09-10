package controller

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

// AppVolumeReconciler projects one internal AppVolume into a durable claim.
type AppVolumeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=platform.molejo.dev,resources=appvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.molejo.dev,resources=appvolumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

func (r *AppVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	result, err := r.reconcile(ctx, req)
	if isCanceledReconciliation(ctx, err) {
		return ctrl.Result{}, nil
	}
	return result, err
}

func (r *AppVolumeReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	volume := &platformv1alpha1.AppVolume{}
	if err := r.Get(ctx, req.NamespacedName, volume); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !volume.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	if volume.Spec.DesiredState == platformv1alpha1.VolumeDesiredDeleted {
		return r.reconcileDeletedVolume(ctx, volume)
	}

	claim, err := r.applyPersistentVolumeClaim(ctx, volume)
	if errors.Is(err, errVolumeOwnershipConflict) {
		return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, volumeOwnershipConflictDecision(), claim)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	decision := evaluateVolume(snapshotPersistentVolumeClaim(claim, volume.Spec.SizeGiB))
	return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, decision, claim)
}

func (r *AppVolumeReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		Named("appvolume").
		For(&platformv1alpha1.AppVolume{}).
		Watches(&corev1.PersistentVolumeClaim{}, handler.EnqueueRequestsFromMapFunc(mapPersistentVolumeClaimToAppVolume)).
		Complete(r)
}

func mapPersistentVolumeClaimToAppVolume(
	_ context.Context,
	object client.Object,
) []reconcile.Request {
	owner := object.GetAnnotations()[appVolumeOwnerAnnotation]
	if owner == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      owner,
	}}}
}
