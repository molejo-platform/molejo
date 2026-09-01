package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppVolumeReconciler) updateVolumeStatus(
	ctx context.Context,
	volume *platformv1alpha1.AppVolume,
	decision volumeDecision,
	claim *corev1.PersistentVolumeClaim,
) error {
	before := volume.DeepCopy()
	volume.Status.ObservedGeneration = volume.Generation
	volume.Status.ObservedSizeGiB = decision.observedSizeGiB
	volume.Status.State = decision.state
	if claim != nil {
		volume.Status.ClaimRef = &corev1.LocalObjectReference{Name: claim.Name}
	}
	conditionStatus := metav1.ConditionFalse
	if decision.state == platformv1alpha1.VolumeStateReady || decision.state == platformv1alpha1.VolumeStateRetained {
		conditionStatus = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&volume.Status.Conditions, metav1.Condition{
		Type:               platformv1alpha1.ConditionReady,
		Status:             conditionStatus,
		ObservedGeneration: volume.Generation,
		Reason:             decision.reason,
		Message:            decision.message,
	})
	if equality.Semantic.DeepEqual(before.Status, volume.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, volume, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("patch AppVolume status %s: %w", types.NamespacedName{Namespace: volume.Namespace, Name: volume.Name}, err)
	}
	return nil
}
