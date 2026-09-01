package main

import (
	"context"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestManagerOptionsProtectMetricsEndpoint(t *testing.T) {
	options := managerOptions(":8443", ":8081", false)
	if !options.Metrics.SecureServing {
		t.Error("expected the metrics endpoint to use secure serving")
	}
	if options.Metrics.FilterProvider == nil {
		t.Error("expected the metrics endpoint to enforce authentication and authorization")
	}
}

func TestReadinessRequiresSynchronizedCache(t *testing.T) {
	tests := []struct {
		name         string
		synchronized bool
		wantError    bool
	}{
		{name: "cache not synchronized", synchronized: false, wantError: true},
		{name: "cache synchronized", synchronized: true, wantError: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checker := readinessChecker(fakeCacheSyncer{synced: test.synchronized})
			request := httptest.NewRequest("GET", "/readyz", nil)
			err := checker(request)
			if test.wantError && err == nil {
				t.Fatal("expected readiness to fail")
			}
			if !test.wantError && err != nil {
				t.Fatalf("expected readiness to succeed: %v", err)
			}
		})
	}
}

func TestTracingIsDisabledByDefault(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "")
	if tracingEnabled() {
		t.Fatal("expected tracing to be disabled without explicit configuration")
	}

	provider, shutdown, err := configureTracing(context.Background(), "test")
	if err != nil {
		t.Fatalf("configure no-op tracing: %v", err)
	}
	if provider == nil || shutdown == nil {
		t.Fatal("expected a no-op provider and shutdown function")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown no-op tracing: %v", err)
	}
}

func TestInvalidTracingExporterFailsConfiguration(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "unsupported")
	provider, shutdown, err := configureTracing(context.Background(), "test")
	if err == nil {
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
		t.Fatalf("expected invalid tracing exporter to fail, got provider %T", provider)
	}
}

func TestBuildInfoMetricUsesOnlyBoundedLabels(t *testing.T) {
	metric, err := buildInfo.GetMetricWithLabelValues(version, commit)
	if err != nil {
		t.Fatalf("get build info metric: %v", err)
	}
	written := &dto.Metric{}
	if err := metric.Write(written); err != nil {
		t.Fatalf("write build info metric: %v", err)
	}

	labels := map[string]bool{}
	for _, label := range written.Label {
		labels[label.GetName()] = true
	}
	if len(labels) != 2 || !labels["version"] || !labels["commit"] {
		t.Fatalf("expected only version and commit labels, got %v", labels)
	}
}

type fakeCacheSyncer struct {
	synced bool
}

func (syncer fakeCacheSyncer) WaitForCacheSync(context.Context) bool {
	return syncer.synced
}
