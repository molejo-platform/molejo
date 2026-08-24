package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
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

	for range 2 {
		if err := adapter.EnsureWorkspace(ctx, "fruto-workspaces"); err != nil {
			t.Fatal(err)
		}
		if err := adapter.ApplyDeployment(ctx, "fruto-workspaces", "ap-aaaaaaaaaaaaaaaaaaaa", runtimeTestIntent("envtest")); err != nil {
			t.Fatal(err)
		}
	}

	var namespace corev1.Namespace
	if err := runtimeClient.Get(ctx, client.ObjectKey{Name: "fruto-workspaces"}, &namespace); err != nil {
		t.Fatal(err)
	}
	if namespace.Annotations[controlPlaneOwnerAnnotation] != workspaceOwnerValue {
		t.Fatalf("workspace owner marker = %q", namespace.Annotations[controlPlaneOwnerAnnotation])
	}
	var deployments platformv1alpha1.AppDeploymentList
	if err := runtimeClient.List(ctx, &deployments, client.InNamespace("fruto-workspaces")); err != nil {
		t.Fatal(err)
	}
	if len(deployments.Items) != 1 || deployments.Items[0].Name != "ap-aaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("AppDeployments = %+v", deployments.Items)
	}
}
