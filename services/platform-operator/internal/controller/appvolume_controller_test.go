package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestAppVolumeReconcileCreatesOnePersistentClaim(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "stateful-volume")
	volume := newAppVolume(namespace, "vol-statefulvolume01")
	if err := testClient.Create(ctx, volume); err != nil {
		t.Fatalf("create AppVolume: %v", err)
	}
	reconciler := &AppVolumeReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: volume.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile AppVolume: %v", err)
	}
	claim := &corev1.PersistentVolumeClaim{}
	if err := testClient.Get(ctx, request.NamespacedName, claim); err != nil {
		t.Fatalf("get PersistentVolumeClaim: %v", err)
	}
	if claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName != "test-storage" {
		t.Fatalf("storage class = %v", claim.Spec.StorageClassName)
	}
	if claim.Spec.Resources.Requests.Storage().Value() != 2<<30 {
		t.Fatalf("storage request = %s", claim.Spec.Resources.Requests.Storage().String())
	}
}

func TestAppVolumeReconcileMarksMissingDeletedClaimRetained(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "volume-retained")
	volume := newAppVolume(namespace, "vol-retained01")
	volume.Spec.DesiredState = platformv1alpha1.VolumeDesiredDeleted
	if err := testClient.Create(ctx, volume); err != nil {
		t.Fatalf("create AppVolume: %v", err)
	}

	reconciler := &AppVolumeReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: volume.Name, Namespace: namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile AppVolume: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("result = %#v", result)
	}

	stored := &platformv1alpha1.AppVolume{}
	if err := testClient.Get(ctx, request.NamespacedName, stored); err != nil {
		t.Fatalf("get AppVolume: %v", err)
	}
	if stored.Status.State != platformv1alpha1.VolumeStateRetained || stored.Status.ClaimRef != nil {
		t.Fatalf("status = %#v", stored.Status)
	}
	ready := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.Reason != platformv1alpha1.ReasonVolumeRetained {
		t.Fatalf("ready condition = %#v", ready)
	}
}

func TestAppVolumeReconcileReportsForeignClaimOwnership(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "volume-ownership")
	volume := newAppVolume(namespace, "vol-ownership01")
	if err := testClient.Create(ctx, volume); err != nil {
		t.Fatalf("create AppVolume: %v", err)
	}
	storageClassName := volume.Spec.StorageClassName
	volumeMode := corev1.PersistentVolumeFilesystem
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: volume.Name, Namespace: namespace,
			Annotations: map[string]string{appVolumeOwnerAnnotation: "vol-anotherowner"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClassName,
			VolumeMode:       &volumeMode,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("2Gi"),
			}},
		},
	}
	if err := testClient.Create(ctx, claim); err != nil {
		t.Fatalf("create foreign PersistentVolumeClaim: %v", err)
	}

	reconciler := &AppVolumeReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: volume.Name, Namespace: namespace}}
	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("reconcile AppVolume: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("result = %#v", result)
	}

	stored := &platformv1alpha1.AppVolume{}
	if err := testClient.Get(ctx, request.NamespacedName, stored); err != nil {
		t.Fatalf("get AppVolume: %v", err)
	}
	if stored.Status.State != platformv1alpha1.VolumeStateDegraded || stored.Status.ClaimRef == nil || stored.Status.ClaimRef.Name != claim.Name {
		t.Fatalf("status = %#v", stored.Status)
	}
	ready := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionReady)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != platformv1alpha1.ReasonVolumeFailed {
		t.Fatalf("ready condition = %#v", ready)
	}
}

func newAppVolume(namespace string, name string) *platformv1alpha1.AppVolume {
	return &platformv1alpha1.AppVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: platformv1alpha1.AppVolumeSpec{
			StorageClassName: "test-storage",
			SizeGiB:          2,
			RetentionPolicy:  platformv1alpha1.VolumeRetentionPreserve,
		},
	}
}
