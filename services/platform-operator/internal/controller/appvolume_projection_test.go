package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestConfigurePersistentVolumeClaimProjectsStorageWithoutClusterDependencies(t *testing.T) {
	volume := newAppVolume("workspace", "vol-projection01")
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"retained-annotation": "true"},
			Labels:      map[string]string{"retained-label": "true"},
		},
	}

	configurePersistentVolumeClaim(claim, volume)

	if claim.Annotations[appVolumeOwnerAnnotation] != volume.Name || claim.Annotations["retained-annotation"] != "true" {
		t.Fatalf("annotations = %#v", claim.Annotations)
	}
	if claim.Labels[managedByLabel] != managedByValue || claim.Labels["retained-label"] != "true" {
		t.Fatalf("labels = %#v", claim.Labels)
	}
	if len(claim.Spec.AccessModes) != 1 || claim.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Fatalf("access modes = %#v", claim.Spec.AccessModes)
	}
	if claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName != volume.Spec.StorageClassName {
		t.Fatalf("storage class = %v", claim.Spec.StorageClassName)
	}
	if claim.Spec.Resources.Requests.Storage().Value() != volume.Spec.SizeGiB<<30 {
		t.Fatalf("storage request = %s", claim.Spec.Resources.Requests.Storage().String())
	}
	if claim.Spec.VolumeMode == nil || *claim.Spec.VolumeMode != corev1.PersistentVolumeFilesystem {
		t.Fatalf("volume mode = %v", claim.Spec.VolumeMode)
	}
}

func TestEvaluateVolumeUsesClaimPhaseAndObservedCapacity(t *testing.T) {
	tests := []struct {
		name     string
		snapshot volumeSnapshot
		want     volumeDecision
	}{
		{
			name:     "pending claim is provisioning",
			snapshot: volumeSnapshot{desiredSizeGiB: 2, phase: corev1.ClaimPending},
			want: volumeDecision{
				state: platformv1alpha1.VolumeStateProvisioning, reason: platformv1alpha1.ReasonVolumeProvisioning,
				message: "Persistent storage is being prepared.",
			},
		},
		{
			name:     "bound claim at desired capacity is ready",
			snapshot: volumeSnapshot{desiredSizeGiB: 2, observedSizeGiB: 2, phase: corev1.ClaimBound},
			want: volumeDecision{
				state: platformv1alpha1.VolumeStateReady, reason: platformv1alpha1.ReasonVolumeReady,
				message: "Persistent storage is ready.", observedSizeGiB: 2,
			},
		},
		{
			name:     "bound claim below desired capacity is expanding",
			snapshot: volumeSnapshot{desiredSizeGiB: 3, observedSizeGiB: 2, phase: corev1.ClaimBound},
			want: volumeDecision{
				state: platformv1alpha1.VolumeStateExpanding, reason: platformv1alpha1.ReasonVolumeExpanding,
				message: "Persistent storage expansion is in progress.", observedSizeGiB: 2,
			},
		},
		{
			name:     "lost claim is degraded",
			snapshot: volumeSnapshot{desiredSizeGiB: 2, observedSizeGiB: 1, phase: corev1.ClaimLost},
			want: volumeDecision{
				state: platformv1alpha1.VolumeStateDegraded, reason: platformv1alpha1.ReasonVolumeFailed,
				message: "Persistent storage is unavailable.", observedSizeGiB: 1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := evaluateVolume(test.snapshot); got != test.want {
				t.Fatalf("decision = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestSnapshotPersistentVolumeClaimUsesBinaryGiB(t *testing.T) {
	claim := &corev1.PersistentVolumeClaim{Status: corev1.PersistentVolumeClaimStatus{
		Phase: corev1.ClaimBound,
		Capacity: corev1.ResourceList{
			corev1.ResourceStorage: resource.MustParse("3Gi"),
		},
	}}

	snapshot := snapshotPersistentVolumeClaim(claim, 4)
	if snapshot.desiredSizeGiB != 4 || snapshot.observedSizeGiB != 3 || snapshot.phase != corev1.ClaimBound {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestMapPersistentVolumeClaimToAppVolumeUsesTheOwnerAnnotation(t *testing.T) {
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
		Name: "claim", Namespace: "workspace",
		Annotations: map[string]string{appVolumeOwnerAnnotation: "vol-owner01"},
	}}

	requests := mapPersistentVolumeClaimToAppVolume(context.Background(), claim)
	if len(requests) != 1 || requests[0].Namespace != claim.Namespace || requests[0].Name != "vol-owner01" {
		t.Fatalf("requests = %#v", requests)
	}
	delete(claim.Annotations, appVolumeOwnerAnnotation)
	if requests := mapPersistentVolumeClaimToAppVolume(context.Background(), claim); requests != nil {
		t.Fatalf("requests without owner annotation = %#v", requests)
	}
}
