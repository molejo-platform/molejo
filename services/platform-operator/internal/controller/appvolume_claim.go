package controller

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

const appVolumeOwnerAnnotation = "platform.molejo.dev/app-volume-owner"

var errVolumeOwnershipConflict = errors.New("persistent volume claim is managed by another AppVolume")

func (r *AppVolumeReconciler) applyPersistentVolumeClaim(
	ctx context.Context,
	volume *platformv1alpha1.AppVolume,
) (*corev1.PersistentVolumeClaim, error) {
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: volume.Name, Namespace: volume.Namespace,
	}}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, claim, func() error {
		if !claim.CreationTimestamp.IsZero() && !persistentVolumeClaimOwnedBy(claim, volume) {
			return errVolumeOwnershipConflict
		}
		configurePersistentVolumeClaim(claim, volume)
		return nil
	})
	return claim, err
}

func configurePersistentVolumeClaim(
	claim *corev1.PersistentVolumeClaim,
	volume *platformv1alpha1.AppVolume,
) {
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
}

func persistentVolumeClaimOwnedBy(
	claim *corev1.PersistentVolumeClaim,
	volume *platformv1alpha1.AppVolume,
) bool {
	return claim.Annotations[appVolumeOwnerAnnotation] == volume.Name
}

func (r *AppVolumeReconciler) reconcileDeletedVolume(
	ctx context.Context,
	volume *platformv1alpha1.AppVolume,
) (ctrl.Result, error) {
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: volume.Name, Namespace: volume.Namespace,
	}}
	if err := r.Get(ctx, client.ObjectKeyFromObject(claim), claim); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, retainedVolumeDecision(), nil)
		}
		return ctrl.Result{}, err
	}
	if !persistentVolumeClaimOwnedBy(claim, volume) {
		return ctrl.Result{}, r.updateVolumeStatus(ctx, volume, volumeOwnershipConflictDecision(), claim)
	}
	if err := r.Delete(ctx, claim); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}
