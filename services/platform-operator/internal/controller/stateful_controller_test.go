package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestAppVolumeReconcileCreatesOnePersistentClaim(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "stateful-volume")
	volume := &platformv1alpha1.AppVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "vol-statefulvolume01", Namespace: namespace},
		Spec: platformv1alpha1.AppVolumeSpec{
			StorageClassName: "test-storage",
			SizeGiB:          2,
			RetentionPolicy:  platformv1alpha1.VolumeRetentionPreserve,
		},
	}
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

func TestStatefulAppDeploymentCreatesStatefulSetWithIndependentVolume(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "stateful-workload")
	volume := &platformv1alpha1.AppVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "vol-statefulworkload1", Namespace: namespace},
		Spec: platformv1alpha1.AppVolumeSpec{
			StorageClassName: "test-storage",
			SizeGiB:          1,
			RetentionPolicy:  platformv1alpha1.VolumeRetentionPreserve,
		},
	}
	if err := testClient.Create(ctx, volume); err != nil {
		t.Fatalf("create AppVolume: %v", err)
	}
	target := newAppDeployment(namespace, "ap-statefulworkload", testImage)
	target.Spec.Workload = platformv1alpha1.AppDeploymentWorkload{
		Kind: platformv1alpha1.WorkloadStateful,
		Stateful: &platformv1alpha1.StatefulWorkload{
			VolumeRef: volume.Name,
			MountPath: "/data",
		},
	}
	if err := testClient.Create(ctx, target); err != nil {
		t.Fatalf("create Stateful AppDeployment: %v", err)
	}
	statefulToleration := corev1.Toleration{
		Key:      "platform.example.com/workload",
		Operator: corev1.TolerationOpEqual,
		Value:    "data",
		Effect:   corev1.TaintEffectNoSchedule,
	}
	reconciler := &AppDeploymentReconciler{
		Client:              testClient,
		Scheme:              testScheme,
		StatefulTolerations: []corev1.Toleration{statefulToleration},
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: target.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile Stateful AppDeployment: %v", err)
	}
	statefulSet := &appsv1.StatefulSet{}
	if err := testClient.Get(ctx, request.NamespacedName, statefulSet); err != nil {
		t.Fatalf("get StatefulSet: %v", err)
	}
	container := statefulSet.Spec.Template.Spec.Containers[0]
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].MountPath != "/data" {
		t.Fatalf("volume mounts = %#v", container.VolumeMounts)
	}
	if statefulSet.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != volume.Name {
		t.Fatalf("workload volume = %#v", statefulSet.Spec.Template.Spec.Volumes)
	}
	if statefulSet.Spec.Template.Spec.SecurityContext == nil || statefulSet.Spec.Template.Spec.SecurityContext.FSGroup == nil || *statefulSet.Spec.Template.Spec.SecurityContext.FSGroup != 65532 {
		t.Fatalf("stateful fsGroup = %#v", statefulSet.Spec.Template.Spec.SecurityContext)
	}
	if len(statefulSet.Spec.Template.Spec.Tolerations) != 1 || statefulSet.Spec.Template.Spec.Tolerations[0] != statefulToleration {
		t.Fatalf("stateful tolerations = %#v", statefulSet.Spec.Template.Spec.Tolerations)
	}
	if err := testClient.Get(ctx, request.NamespacedName, &appsv1.Deployment{}); err == nil {
		t.Fatal("stateful workload must not create a Deployment")
	}
}

func TestStatefulSchedulingDoesNotAffectStatelessDeployment(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "stateless-scheduling")
	target := newAppDeployment(namespace, "ap-statelessscheduling", testImage)
	if err := testClient.Create(ctx, target); err != nil {
		t.Fatalf("create stateless AppDeployment: %v", err)
	}
	reconciler := &AppDeploymentReconciler{
		Client: testClient,
		Scheme: testScheme,
		StatefulTolerations: []corev1.Toleration{{
			Key:      "platform.example.com/workload",
			Operator: corev1.TolerationOpEqual,
			Value:    "data",
			Effect:   corev1.TaintEffectNoSchedule,
		}},
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: target.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile stateless AppDeployment: %v", err)
	}
	deployment := &appsv1.Deployment{}
	if err := testClient.Get(ctx, request.NamespacedName, deployment); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	if len(deployment.Spec.Template.Spec.Tolerations) != 0 {
		t.Fatalf("stateless tolerations = %#v", deployment.Spec.Template.Spec.Tolerations)
	}
}
