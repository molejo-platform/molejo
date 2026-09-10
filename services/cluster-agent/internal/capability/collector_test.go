package capability

import (
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

func TestProbeOutcomeDoesNotExposeRawErrors(t *testing.T) {
	now := time.Now().UTC()
	base := observation(capabilitycontract.RuntimeEventsCurrent, now)
	tests := []struct {
		name   string
		err    error
		health capabilitycontract.Health
		reason string
	}{
		{name: "healthy", health: capabilitycontract.HealthHealthy},
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", errors.New("secret provider response")), health: capabilitycontract.HealthUnavailable, reason: capabilitycontract.ReasonAccessDenied},
		{name: "unknown", err: errors.New("sensitive endpoint failed"), health: capabilitycontract.HealthUnknown, reason: capabilitycontract.ReasonProbeFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := outcome(base, test.err)
			if got.Health != test.health || got.ReasonCode != test.reason || got.Message != "" {
				t.Fatalf("got health=%s reason=%s message=%q", got.Health, got.ReasonCode, got.Message)
			}
		})
	}
}

func TestSnapshotIsCopied(t *testing.T) {
	collector := &Collector{snapshot: []capabilitycontract.Observation{{ID: capabilitycontract.StorageRWO, Limitations: []string{"one"}}}}
	first, complete := collector.Snapshot()
	if !complete {
		t.Fatal("snapshot is not complete")
	}
	first[0].Limitations[0] = "changed"
	second, _ := collector.Snapshot()
	if second[0].Limitations[0] != "one" {
		t.Fatal("snapshot aliases cached state")
	}
}

func TestCurrentRuntimeCapabilitiesRemainAvailableWithNamespacedAccess(t *testing.T) {
	kubernetesClient := kubernetesfake.NewSimpleClientset()
	discoveryClient := kubernetesClient.Discovery().(*discoveryfake.FakeDiscovery)
	discoveryClient.Resources = []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods/log"}, {Name: "events"}}},
		{GroupVersion: "metrics.k8s.io/v1beta1", APIResources: []metav1.APIResource{{Name: "pods"}}},
	}
	kubernetesClient.PrependReactor("list", "events", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", errors.New("cluster-wide access is intentionally absent"))
	})
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}: "PodMetricsList",
	})
	dynamicClient.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}, "", errors.New("cluster-wide access is intentionally absent"))
	})

	collector := NewCollector(kubernetesClient, discoveryClient, dynamicClient)
	observations := collector.collect(t.Context())
	byID := map[capabilitycontract.ID]capabilitycontract.Observation{}
	for _, observation := range observations {
		byID[observation.ID] = observation
	}
	for _, id := range []capabilitycontract.ID{capabilitycontract.RuntimeLogsCurrent, capabilitycontract.RuntimeEventsCurrent, capabilitycontract.RuntimeMetricsCurrent} {
		if observation := byID[id]; observation.Health != capabilitycontract.HealthHealthy || observation.ReasonCode != "" {
			t.Fatalf("%s observation=%+v", id, observation)
		}
	}
}
