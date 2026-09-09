// Package historicalmetrics owns the explicit binding between a runtime cluster
// and the provider used for historical application metrics.
package historicalmetrics

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
)

const ProviderPrometheusCompatible = "PrometheusCompatible"

var ErrNotFound = errors.New("historical metric binding not found")

type Cluster struct {
	ID  string
	UID string
}

type Binding struct {
	ClusterID   string
	ClusterUID  string
	Provider    string
	Endpoint    string
	Health      providerbinding.Health
	Conformant  bool
	ReasonCode  string
	Limitations []string
	ObservedAt  *time.Time
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Repository interface {
	HistoricalMetricCluster(context.Context, string) (Cluster, error)
	HistoricalMetricBinding(context.Context, string) (Binding, error)
	PutHistoricalMetricBinding(context.Context, string, string, observability.MetricConformanceEvidence, int64, *int64, audit.Event) (Binding, error)
	DeleteHistoricalMetricBinding(context.Context, string, int64, int64, audit.Event) error
}

type prometheusAdapter interface {
	observability.HistoricalMetricReader
	CheckConformance(context.Context, observability.Scope) (observability.MetricConformanceEvidence, error)
}

type adapterFactory func(string, *http.Client) (prometheusAdapter, error)

type Service struct {
	repository Repository
	http       *http.Client
	adapter    adapterFactory
}

func NewService(repository Repository, httpClient *http.Client) *Service {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{
		repository: repository,
		http:       httpClient,
		adapter: func(endpoint string, client *http.Client) (prometheusAdapter, error) {
			return observability.NewPrometheusQueryAdapter(endpoint, client)
		},
	}
}

func NormalizeEndpoint(value string) (string, error) {
	if len(strings.TrimSpace(value)) > 2048 {
		return "", errors.New("invalid Prometheus-compatible endpoint")
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid Prometheus-compatible endpoint")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func (s *Service) Configure(ctx context.Context, clusterID, endpoint string, actorID int64, expectedVersion *int64, event audit.Event) (Binding, error) {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return Binding{}, err
	}
	cluster, err := s.repository.HistoricalMetricCluster(ctx, clusterID)
	if err != nil {
		return Binding{}, err
	}
	if cluster.UID == "" {
		return Binding{}, errors.New("cluster identity is unavailable")
	}
	adapter, err := s.adapter(endpoint, s.http)
	if err != nil {
		return Binding{}, err
	}
	evidence, checkErr := adapter.CheckConformance(ctx, observability.Scope{ClusterID: cluster.ID, ClusterUID: cluster.UID})
	if checkErr != nil {
		evidence = observability.MetricConformanceEvidence{ReasonCode: "metrics_provider_unreachable", Limitations: []string{"historical_metrics_provider_unreachable"}, ObservedAt: time.Now().UTC()}
	}
	if evidence.Limitations == nil {
		evidence.Limitations = []string{}
	}
	return s.repository.PutHistoricalMetricBinding(ctx, clusterID, endpoint, evidence, actorID, expectedVersion, event)
}

func (s *Service) Get(ctx context.Context, clusterID string) (Binding, error) {
	return s.repository.HistoricalMetricBinding(ctx, clusterID)
}

func (s *Service) Delete(ctx context.Context, clusterID string, actorID, expectedVersion int64, event audit.Event) error {
	return s.repository.DeleteHistoricalMetricBinding(ctx, clusterID, actorID, expectedVersion, event)
}

func (s *Service) Metrics(ctx context.Context, scope observability.Scope, query observability.MetricQuery) (observability.Metrics, error) {
	binding, err := s.repository.HistoricalMetricBinding(ctx, scope.ClusterID)
	if err != nil || binding.Health != providerbinding.HealthHealthy || !binding.Conformant || binding.ClusterUID == "" || binding.ClusterUID != scope.ClusterUID {
		return observability.Metrics{}, observability.ErrUnavailable
	}
	adapter, err := s.adapter(binding.Endpoint, s.http)
	if err != nil {
		return observability.Metrics{}, observability.ErrUnavailable
	}
	return adapter.Metrics(ctx, scope, query)
}

func ProviderFact(binding Binding) providerbinding.Binding {
	return providerbinding.Binding{
		Capability:          capabilitycontract.TelemetryMetricsHistorical,
		Configured:          true,
		Health:              binding.Health,
		ConformanceRequired: true,
		Conformant:          binding.Conformant,
		ReasonCode:          binding.ReasonCode,
		Limitations:         append([]string{}, binding.Limitations...),
		ObservedAt:          binding.ObservedAt,
	}
}
