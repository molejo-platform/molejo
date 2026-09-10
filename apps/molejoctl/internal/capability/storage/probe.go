package storage

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "molejoctl"
	partOfLabel    = "app.kubernetes.io/part-of"
	partOfValue    = "molejo"
)

func probeResources(namespace, storageClassName, image string) (*corev1.PersistentVolumeClaim, *corev1.Pod) {
	runAsUser := int64(65532)
	labels := map[string]string{managedByLabel: managedByValue, partOfLabel: partOfValue}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: namespace, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &storageClassName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("16Mi")},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "writer", Namespace: namespace, Labels: labels},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: pointerTo(false),
			RestartPolicy:                corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   pointerTo(true),
				RunAsUser:      &runAsUser,
				RunAsGroup:     &runAsUser,
				FSGroup:        &runAsUser,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:    "writer",
				Image:   image,
				Command: []string{"sh", "-c", "printf molejo-storage-smoke > /data/probe && test \"$(cat /data/probe)\" = molejo-storage-smoke"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("5m"), corev1.ResourceMemory: resource.MustParse("8Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: pointerTo(false),
					ReadOnlyRootFilesystem:   pointerTo(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
			}},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.Name}}}},
		},
	}
	return pvc, pod
}

func smokeNamespace(runID string) string {
	return fmt.Sprintf("molejo-storage-smoke-%s", runID)
}

func pointerTo[T any](value T) *T { return &value }
