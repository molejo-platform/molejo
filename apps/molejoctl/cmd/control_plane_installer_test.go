package cmd

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRequireCompatibleClusterAgent(t *testing.T) {
	optional := true
	compatible := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "cluster-agent", Namespace: systemNamespace}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "agent", EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cluster-agent-connection"}, Optional: &optional}}},
	}}}}}}
	if err := requireClusterAgent(t.Context(), fake.NewClientset(compatible)); err != nil {
		t.Fatalf("compatible Agent rejected: %v", err)
	}
	incompatible := compatible.DeepCopy()
	incompatible.Spec.Template.Spec.Containers[0].EnvFrom = nil
	if err := requireClusterAgent(t.Context(), fake.NewClientset(incompatible)); err == nil || !strings.Contains(err.Error(), "reinstall") {
		t.Fatalf("incompatible Agent error=%v", err)
	}
}

func TestResolvePostgresStorageClass(t *testing.T) {
	defaultClass := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "local-path", Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}}}
	client := fake.NewClientset(defaultClass)
	resolved, err := resolvePostgresStorageClass(t.Context(), client, "", false)
	if err != nil || resolved != "" {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	if _, err = resolvePostgresStorageClass(t.Context(), fake.NewClientset(), "", false); err == nil || !strings.Contains(err.Error(), "no default") {
		t.Fatalf("missing default error=%v", err)
	}
}

func TestDatabaseCredentialIsGeneratedOnce(t *testing.T) {
	client := fake.NewClientset()
	first, err := ensureDatabaseSecret(t.Context(), client, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureDatabaseSecret(t.Context(), client, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.password == "" || first.password != second.password {
		t.Fatalf("database credential was rotated: first=%q second=%q", first.password, second.password)
	}
}
