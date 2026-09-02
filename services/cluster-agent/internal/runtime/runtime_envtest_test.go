package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
)

func TestEnvtestEnsuresExactWorkspaceAndAppDeploymentIdempotently(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is required for envtest")
	}
	environment := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../deploy/crds"}, ErrorIfCRDPathMissing: true}
	config, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})
	scheme := k8sruntime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	runtimeClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &KubernetesClient{client: runtimeClient, fieldManager: "envtest-control-plane", applyTimeout: 5 * time.Second}
	ctx := context.Background()
	intent := runtimeTestIntent("envtest")
	intent.Variables = []runtimecontract.Variable{{Name: "APP_MODE", Value: "test"}}
	intent.ConfigurationVersion = 3
	intent.SecretVariables = []runtimecontract.Variable{{Name: "API_TOKEN", Value: "runtime-only-secret"}}

	for range 2 {
		if err := adapter.EnsureWorkspace(ctx, "molejo-workspaces"); err != nil {
			t.Fatal(err)
		}
		if err := adapter.ApplyDeployment(ctx, "molejo-workspaces", "ap-aaaaaaaaaaaaaaaaaaaa", intent); err != nil {
			t.Fatal(err)
		}
	}

	var namespace corev1.Namespace
	if err := runtimeClient.Get(ctx, client.ObjectKey{Name: "molejo-workspaces"}, &namespace); err != nil {
		t.Fatal(err)
	}
	if namespace.Annotations[controlPlaneOwnerAnnotation] != workspaceOwnerValue {
		t.Fatalf("workspace owner marker = %q", namespace.Annotations[controlPlaneOwnerAnnotation])
	}
	var deployments platformv1alpha1.AppDeploymentList
	if err := runtimeClient.List(ctx, &deployments, client.InNamespace("molejo-workspaces")); err != nil {
		t.Fatal(err)
	}
	if len(deployments.Items) != 1 || deployments.Items[0].Name != "ap-aaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("AppDeployments = %+v", deployments.Items)
	}
	deployment := deployments.Items[0]
	if deployment.Spec.ConfigMapRef != "ap-aaaaaaaaaaaaaaaaaaaa-c3" || deployment.Spec.SecretRef != "ap-aaaaaaaaaaaaaaaaaaaa-c3-secret" || len(deployment.Spec.Variables) != 0 {
		t.Fatalf("configuration references = %+v", deployment.Spec)
	}
	var configMap corev1.ConfigMap
	if err := runtimeClient.Get(ctx, client.ObjectKey{Namespace: "molejo-workspaces", Name: deployment.Spec.ConfigMapRef}, &configMap); err != nil {
		t.Fatal(err)
	}
	var secret corev1.Secret
	if err := runtimeClient.Get(ctx, client.ObjectKey{Namespace: "molejo-workspaces", Name: deployment.Spec.SecretRef}, &secret); err != nil {
		t.Fatal(err)
	}
	if configMap.Immutable == nil || !*configMap.Immutable || configMap.Data["APP_MODE"] != "test" || secret.Immutable == nil || !*secret.Immutable || string(secret.Data["API_TOKEN"]) != "runtime-only-secret" {
		t.Fatalf("materialized configuration ConfigMap=%+v Secret=%+v", configMap.Data, secret.Data)
	}
}
