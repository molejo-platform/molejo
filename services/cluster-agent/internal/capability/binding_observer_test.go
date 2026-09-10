package capability

import (
	"testing"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

func TestObserveBindingsReadsOnlyTheSelectedResources(t *testing.T) {
	expand := true
	mode := storagev1.VolumeBindingWaitForFirstConsumer
	kubernetesClient := kubernetesfake.NewSimpleClientset(&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "local-path"}, Provisioner: "rancher.io/local-path", AllowVolumeExpansion: &expand, VolumeBindingMode: &mode})
	gateway := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "Gateway",
		"metadata": map[string]any{"name": "molejo", "namespace": "molejo-system"},
		"spec":     map[string]any{"gatewayClassName": "traefik"},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Programmed", "status": "True"}},
			"listeners":  []any{map[string]any{"name": "https-molejo", "conditions": []any{map[string]any{"type": "Accepted", "status": "True"}, map[string]any{"type": "Programmed", "status": "True"}}, "supportedKinds": []any{map[string]any{"kind": "HTTPRoute"}}}},
		},
	}}
	class := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": "GatewayClass", "metadata": map[string]any{"name": "traefik"},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Accepted", "status": "True"}}},
	}}
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	if _, err := dynamicClient.Resource(gatewayResource).Namespace("molejo-system").Create(t.Context(), gateway, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := dynamicClient.Resource(gatewayClassResource).Create(t.Context(), class, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	collector := NewCollector(kubernetesClient, kubernetesClient.Discovery(), dynamicClient)
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	collector.now = func() time.Time { return now }
	targets := []kubernetesbinding.Target{
		{ID: "storage:persistent-standard", Kind: kubernetesbinding.KindStorage, Version: 2, Storage: &kubernetesbinding.StorageTarget{StorageClassName: "local-path"}},
		{ID: "publication:http", Kind: kubernetesbinding.KindPublicationHTTP, Version: 4, Publication: &kubernetesbinding.PublicationTarget{GatewayNamespace: "molejo-system", GatewayName: "molejo", SectionName: "https-molejo"}},
	}
	observations, complete := collector.ObserveBindings(t.Context(), targets)
	if !complete || len(observations) != 2 {
		t.Fatalf("observations=%+v complete=%t", observations, complete)
	}
	if observations[0].Health != kubernetesbinding.HealthHealthy || observations[0].Storage.Provisioner != "rancher.io/local-path" || !observations[0].Storage.AllowExpansion {
		t.Fatalf("storage observation=%+v", observations[0])
	}
	publication := observations[1]
	if publication.Health != kubernetesbinding.HealthHealthy || !publication.Publication.GatewayClassAccepted || !publication.Publication.GatewayProgrammed || !publication.Publication.ListenerReady {
		t.Fatalf("publication observation=%+v", publication)
	}
}

func TestObserveStorageBindingReportsMissingSelectedClass(t *testing.T) {
	kubernetesClient := kubernetesfake.NewSimpleClientset()
	collector := NewCollector(kubernetesClient, kubernetesClient.Discovery(), fake.NewSimpleDynamicClient(runtime.NewScheme()))
	observations, complete := collector.ObserveBindings(t.Context(), []kubernetesbinding.Target{{ID: "storage:standard", Kind: kubernetesbinding.KindStorage, Version: 1, Storage: &kubernetesbinding.StorageTarget{StorageClassName: "missing"}}})
	if !complete || len(observations) != 1 || observations[0].Health != kubernetesbinding.HealthUnavailable || observations[0].ReasonCode != "binding_resource_not_found" {
		t.Fatalf("observations=%+v complete=%t", observations, complete)
	}
}
