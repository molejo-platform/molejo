package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

const appVolumeOwnerAnnotation = "platform.molejo.dev/app-volume-owner"

var errVolumeOwnershipConflict = errors.New("persistent volume claim is managed by another AppVolume")

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
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: volume.Name, Namespace: volume.Namespace}}
	if volume.Spec.DesiredState == platformv1alpha1.VolumeDesiredDeleted {
		if err := r.Get(ctx, req.NamespacedName, claim); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, platformv1alpha1.VolumeStateRetained, platformv1alpha1.ReasonVolumeRetained, "Persistent data is retained after explicit detach.", 0, nil)
			}
			return ctrl.Result{}, err
		}
		if claim.Annotations[appVolumeOwnerAnnotation] != volume.Name {
			return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, platformv1alpha1.VolumeStateDegraded, platformv1alpha1.ReasonVolumeFailed, "Persistent storage ownership conflicts with another resource.", 0, claim)
		}
		if err := r.Delete(ctx, claim); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	_, err := controllerutil.CreateOrPatch(ctx, r.Client, claim, func() error {
		if !claim.CreationTimestamp.IsZero() && claim.Annotations[appVolumeOwnerAnnotation] != volume.Name {
			return errVolumeOwnershipConflict
		}
		if claim.Annotations == nil {
			claim.Annotations = map[string]string{}
		}
		claim.Annotations[appVolumeOwnerAnnotation] = volume.Name
		if claim.Labels == nil {
			claim.Labels = map[string]string{}
		}
		claim.Labels[managedByLabel] = managedByValue
		storageClassName := volume.Spec.StorageClassName
		claim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		claim.Spec.StorageClassName = &storageClassName
		if claim.Spec.Resources.Requests == nil {
			claim.Spec.Resources.Requests = corev1.ResourceList{}
		}
		claim.Spec.Resources.Requests[corev1.ResourceStorage] = *resource.NewQuantity(volume.Spec.SizeGiB<<30, resource.BinarySI)
		volumeMode := corev1.PersistentVolumeFilesystem
		claim.Spec.VolumeMode = &volumeMode
		return nil
	})
	if errors.Is(err, errVolumeOwnershipConflict) {
		return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, platformv1alpha1.VolumeStateDegraded, platformv1alpha1.ReasonVolumeFailed, "Persistent storage ownership conflicts with another resource.", 0, claim)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	state := platformv1alpha1.VolumeStateProvisioning
	reason := platformv1alpha1.ReasonVolumeProvisioning
	message := "Persistent storage is being prepared."
	observedSize := int64(0)
	if capacity, found := claim.Status.Capacity[corev1.ResourceStorage]; found {
		observedSize = capacity.Value() >> 30
	}
	switch claim.Status.Phase {
	case corev1.ClaimBound:
		if observedSize >= volume.Spec.SizeGiB {
			state, reason, message = platformv1alpha1.VolumeStateReady, platformv1alpha1.ReasonVolumeReady, "Persistent storage is ready."
		} else {
			state, reason, message = platformv1alpha1.VolumeStateExpanding, platformv1alpha1.ReasonVolumeExpanding, "Persistent storage expansion is in progress."
		}
	case corev1.ClaimLost:
		state, reason, message = platformv1alpha1.VolumeStateDegraded, platformv1alpha1.ReasonVolumeFailed, "Persistent storage is unavailable."
	}
	return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, state, reason, message, observedSize, claim)
}

func (r *AppVolumeReconciler) updateVolumeStatus(ctx context.Context, volume *platformv1alpha1.AppVolume, state platformv1alpha1.AppVolumeState, reason, message string, observedSize int64, claim *corev1.PersistentVolumeClaim) error {
	before := volume.DeepCopy()
	volume.Status.ObservedGeneration = volume.Generation
	volume.Status.ObservedSizeGiB = observedSize
	volume.Status.State = state
	if claim != nil {
		volume.Status.ClaimRef = &corev1.LocalObjectReference{Name: claim.Name}
	}
	conditionStatus := metav1.ConditionFalse
	if state == platformv1alpha1.VolumeStateReady || state == platformv1alpha1.VolumeStateRetained {
		conditionStatus = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&volume.Status.Conditions, metav1.Condition{Type: platformv1alpha1.ConditionReady, Status: conditionStatus, ObservedGeneration: volume.Generation, Reason: reason, Message: message})
	if equality.Semantic.DeepEqual(before.Status, volume.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, volume, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("patch AppVolume status %s: %w", types.NamespacedName{Namespace: volume.Namespace, Name: volume.Name}, err)
	}
	return nil
}

func (r *AppVolumeReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		Named("appvolume").
		For(&platformv1alpha1.AppVolume{}).
		Watches(&corev1.PersistentVolumeClaim{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, object client.Object) []reconcile.Request {
			owner := object.GetAnnotations()[appVolumeOwnerAnnotation]
			if owner == "" {
				return nil
			}
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: object.GetNamespace(), Name: owner}}}
		})).
		Complete(r)
}
