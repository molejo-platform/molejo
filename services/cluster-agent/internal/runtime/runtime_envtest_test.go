package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
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
	destination := kubernetesbinding.HTTPDestination{BindingID: "binding-one", BindingRevision: 3, SchemaVersion: kubernetesbinding.HTTPBindingSchemaVersion, GatewayNamespace: "external-edge", GatewayName: "shared", SectionName: "apex"}
	intent.PublicEndpoints = []runtimecontract.PublicEndpoint{{Name: "web", Type: "HTTP", PortName: "http", Addresses: []runtimecontract.HTTPAddress{{Hostname: "example.test", Destination: destination}, {Hostname: "www.example.test", Destination: destination}}}}
	intent.PublicEndpoints[0].Addresses[1].Destination.SectionName = "pool"
	intent.Variables = []runtimecontract.Variable{{Name: "APP_MODE", Value: "test"}}
	intent.ConfigurationVersion = 3
	intent.SecretVariables = []runtimecontract.Variable{{Name: "API_TOKEN", Value: "runtime-only-secret"}}
	workspaceNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ws-abcdefghijklmnopqrst", Annotations: map[string]string{controlPlaneOwnerAnnotation: workspaceOwnerValue}}}
	if err := runtimeClient.Create(ctx, workspaceNamespace); err != nil {
		t.Fatal(err)
	}
	placementIntent := runtimecontract.WorkspacePlacementIntent{WorkspaceID: "ws-abcdefghijklmnopqrst", NamespaceName: "ws-abcdefghijklmnopqrst", AccessProfile: "NamespacedRuntime", LifecycleState: "Ready"}

	for range 2 {
		if _, err := adapter.EnsureWorkspacePlacement(ctx, placementIntent); err != nil {
			t.Fatal(err)
		}
		if err := adapter.ApplyDeployment(ctx, placementIntent.NamespaceName, "ap-aaaaaaaaaaaaaaaaaaaa", 1, intent); err != nil {
			t.Fatal(err)
		}
	}

	var namespace corev1.Namespace
	if err := runtimeClient.Get(ctx, client.ObjectKey{Name: placementIntent.NamespaceName}, &namespace); err != nil {
		t.Fatal(err)
	}
	if namespace.Annotations[controlPlaneOwnerAnnotation] != workspaceOwnerValue {
		t.Fatalf("workspace owner marker = %q", namespace.Annotations[controlPlaneOwnerAnnotation])
	}
	var deployments platformv1alpha1.AppDeploymentList
	if err := runtimeClient.List(ctx, &deployments, client.InNamespace(placementIntent.NamespaceName)); err != nil {
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
	if err := runtimeClient.Get(ctx, client.ObjectKey{Namespace: placementIntent.NamespaceName, Name: deployment.Spec.ConfigMapRef}, &configMap); err != nil {
		t.Fatal(err)
	}
	var secret corev1.Secret
	if err := runtimeClient.Get(ctx, client.ObjectKey{Namespace: placementIntent.NamespaceName, Name: deployment.Spec.SecretRef}, &secret); err != nil {
		t.Fatal(err)
	}
	if configMap.Immutable == nil || !*configMap.Immutable || configMap.Data["APP_MODE"] != "test" || secret.Immutable == nil || !*secret.Immutable || string(secret.Data["API_TOKEN"]) != "runtime-only-secret" {
		t.Fatalf("materialized configuration ConfigMap=%+v Secret=%+v", configMap.Data, secret.Data)
	}
	if len(deployment.Spec.PublicEndpoints) != 1 || len(deployment.Spec.PublicEndpoints[0].Addresses) != 2 || deployment.Spec.PublicEndpoints[0].Addresses[1].Destination.SectionName != "pool" {
		t.Fatal("address destination was truncated")
	}
	a := deployment.Spec.PublicEndpoints[0].Addresses[0]
	deployment.Status.EndpointStatuses = []platformv1alpha1.AppDeploymentEndpointStatus{{Name: "web", Type: "HTTP", Addresses: []platformv1alpha1.AppDeploymentHTTPAddressStatus{{Hostname: a.Hostname, Destination: a.Destination, RouteName: "route-one", RouteUID: "route-uid", RouteGeneration: 2, GatewayUID: "gateway-uid", Conditions: []metav1.Condition{{Type: "RouteReady", Status: metav1.ConditionTrue, Reason: "Ready", ObservedGeneration: deployment.Generation, LastTransitionTime: metav1.Now()}}}}}}
	if err := runtimeClient.Status().Update(ctx, &deployment); err != nil {
		t.Fatal(err)
	}
	observations, err := adapter.RuntimeObservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || len(observations[0].GetAddresses()) != 1 {
		t.Fatal("address evidence not transported")
	}
	wire, err := proto.Marshal(observations[0])
	if err != nil {
		t.Fatal(err)
	}
	decoded := &clusteragentv1alpha1.RuntimeObservation{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetUid() != string(deployment.UID) || decoded.GetAddresses()[0].GetBindingId() != destination.BindingID || decoded.GetAddresses()[0].GetGatewayUid() != "gateway-uid" {
		t.Fatal("wire correlation lost")
	}
	// Entire invalid allocation must fail before configuration materialization.
	intent.ConfigurationVersion = 4
	intent.PublicEndpoints[0].Addresses[0].Destination.SchemaVersion = "unsupported"
	if err := adapter.ApplyDeployment(ctx, placementIntent.NamespaceName, "ap-aaaaaaaaaaaaaaaaaaaa", 2, intent); err == nil {
		t.Fatal("invalid schema accepted")
	}
	if err := runtimeClient.Get(ctx, client.ObjectKey{Namespace: placementIntent.NamespaceName, Name: "ap-aaaaaaaaaaaaaaaaaaaa-c4"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("invalid allocation had side effects: %v", err)
	}
}
