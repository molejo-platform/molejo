package historicalmetrics

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
)

type repositoryStub struct {
	cluster Cluster
	binding Binding
}

func (r *repositoryStub) HistoricalMetricCluster(context.Context, string) (Cluster, error) {
	return r.cluster, nil
}

func (r *repositoryStub) HistoricalMetricBinding(context.Context, string) (Binding, error) {
	return r.binding, nil
}

func (r *repositoryStub) PutHistoricalMetricBinding(_ context.Context, clusterID, endpoint string, evidence observability.MetricConformanceEvidence, _ int64, _ *int64, _ audit.Event) (Binding, error) {
	r.binding = Binding{ClusterID: clusterID, ClusterUID: r.cluster.UID, Endpoint: endpoint, Health: providerbinding.HealthHealthy, Conformant: evidence.Conformant}
	return r.binding, nil
}

func (*repositoryStub) DeleteHistoricalMetricBinding(context.Context, string, int64, int64, audit.Event) error {
	return nil
}

type adapterStub struct {
	evidence observability.MetricConformanceEvidence
	metrics  observability.Metrics
}

func (a adapterStub) CheckConformance(context.Context, observability.Scope) (observability.MetricConformanceEvidence, error) {
	return a.evidence, nil
}

func (a adapterStub) Metrics(context.Context, observability.Scope, observability.MetricQuery) (observability.Metrics, error) {
	return a.metrics, nil
}

func TestServiceRefusesHistoricalReadsWithoutConformantMatchingBinding(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
	}{
		{name: "unproven", binding: Binding{ClusterUID: "uid-a", Health: providerbinding.HealthUnknown}},
		{name: "unhealthy", binding: Binding{ClusterUID: "uid-a", Health: providerbinding.HealthUnavailable, Conformant: true}},
		{name: "different cluster incarnation", binding: Binding{ClusterUID: "uid-old", Health: providerbinding.HealthHealthy, Conformant: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&repositoryStub{binding: test.binding}, nil)
			_, err := service.Metrics(t.Context(), observability.Scope{ClusterID: "cls-a", ClusterUID: "uid-a"}, observability.MetricQuery{})
			if !errors.Is(err, observability.ErrUnavailable) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestServiceConfiguresNormalizedEndpointAfterConformance(t *testing.T) {
	repository := &repositoryStub{cluster: Cluster{ID: "cls-a", UID: "uid-a"}}
	service := NewService(repository, nil)
	service.adapter = func(string, *http.Client) (prometheusAdapter, error) {
		return adapterStub{evidence: observability.MetricConformanceEvidence{Conformant: true}}, nil
	}
	binding, err := service.Configure(t.Context(), "cls-a", "https://metrics.example.test/prometheus/", 1, nil, audit.Event{})
	if err != nil || binding.Endpoint != "https://metrics.example.test/prometheus" || !binding.Conformant {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
}

func TestNormalizeEndpointRejectsEmbeddedCredentials(t *testing.T) {
	if _, err := NormalizeEndpoint("https://user:secret@metrics.example.test"); err == nil {
		t.Fatal("embedded credentials were accepted")
	}
}
