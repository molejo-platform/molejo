package controller

import (
	corev1 "k8s.io/api/core/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

type volumeSnapshot struct {
	desiredSizeGiB  int64
	observedSizeGiB int64
	phase           corev1.PersistentVolumeClaimPhase
}

type volumeDecision struct {
	state           platformv1alpha1.AppVolumeState
	reason          string
	message         string
	observedSizeGiB int64
}

func snapshotPersistentVolumeClaim(
	claim *corev1.PersistentVolumeClaim,
	desiredSizeGiB int64,
) volumeSnapshot {
	observedSizeGiB := int64(0)
	if capacity, found := claim.Status.Capacity[corev1.ResourceStorage]; found {
		observedSizeGiB = capacity.Value() >> 30
	}
	return volumeSnapshot{
		desiredSizeGiB:  desiredSizeGiB,
		observedSizeGiB: observedSizeGiB,
		phase:           claim.Status.Phase,
	}
}

func evaluateVolume(snapshot volumeSnapshot) volumeDecision {
	decision := volumeDecision{
		state:           platformv1alpha1.VolumeStateProvisioning,
		reason:          platformv1alpha1.ReasonVolumeProvisioning,
		message:         "Persistent storage is being prepared.",
		observedSizeGiB: snapshot.observedSizeGiB,
	}
	switch snapshot.phase {
	case corev1.ClaimBound:
		if snapshot.observedSizeGiB >= snapshot.desiredSizeGiB {
			decision.state = platformv1alpha1.VolumeStateReady
			decision.reason = platformv1alpha1.ReasonVolumeReady
			decision.message = "Persistent storage is ready."
		} else {
			decision.state = platformv1alpha1.VolumeStateExpanding
			decision.reason = platformv1alpha1.ReasonVolumeExpanding
			decision.message = "Persistent storage expansion is in progress."
		}
	case corev1.ClaimLost:
		decision.state = platformv1alpha1.VolumeStateDegraded
		decision.reason = platformv1alpha1.ReasonVolumeFailed
		decision.message = "Persistent storage is unavailable."
	}
	return decision
}

func retainedVolumeDecision() volumeDecision {
	return volumeDecision{
		state:   platformv1alpha1.VolumeStateRetained,
		reason:  platformv1alpha1.ReasonVolumeRetained,
		message: "Persistent data is retained after explicit detach.",
	}
}

func volumeOwnershipConflictDecision() volumeDecision {
	return volumeDecision{
		state:   platformv1alpha1.VolumeStateDegraded,
		reason:  platformv1alpha1.ReasonVolumeFailed,
		message: "Persistent storage ownership conflicts with another resource.",
	}
}
