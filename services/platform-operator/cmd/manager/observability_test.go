package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"sigs.k8s.io/yaml"
)

func TestManagerOptionsProtectMetricsEndpoint(t *testing.T) {
	options := managerOptions(":8443", ":8081", false)
	if !options.Metrics.SecureServing {
		t.Error("expected the metrics endpoint to use secure serving")
	}
	if options.Metrics.FilterProvider == nil {
		t.Error("expected the metrics endpoint to enforce authentication and authorization")
	}
	if options.GracefulShutdownTimeout == nil || *options.GracefulShutdownTimeout != managerGracefulShutdownTimeout {
		t.Fatalf("graceful shutdown timeout = %v, want %s", options.GracefulShutdownTimeout, managerGracefulShutdownTimeout)
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
			checker := readinessChecker(context.Background(), &fakeCacheSyncer{synced: test.synchronized})
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

func TestReadinessFailsWithoutConsultingCacheAfterShutdownBegins(t *testing.T) {
	processCtx, cancel := context.WithCancel(context.Background())
	cancel()
	syncer := &fakeCacheSyncer{synced: true}

	err := readinessChecker(processCtx, syncer)(httptest.NewRequest("GET", "/readyz", nil))
	if err == nil {
		t.Fatal("expected readiness to fail after shutdown begins")
	}
	if syncer.calls != 0 {
		t.Fatalf("cache sync calls = %d, want 0", syncer.calls)
	}
}

func TestFlushTracingUsesFreshBoundedContext(t *testing.T) {
	err := flushTracing(func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			t.Fatalf("flush context is already canceled: %v", err)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("flush context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > tracingShutdownTimeout {
			t.Fatalf("flush deadline remaining = %s, want within (0, %s]", remaining, tracingShutdownTimeout)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("flush tracing: %v", err)
	}
}

func TestShutdownBudgetFitsDeploymentTerminationGracePeriod(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "operator", "manager", "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := struct {
		Spec struct {
			Template struct {
				Spec struct {
					TerminationGracePeriodSeconds int64 `yaml:"terminationGracePeriodSeconds"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}{}
	if err := yaml.Unmarshal(contents, &manifest); err != nil {
		t.Fatal(err)
	}

	podGrace := time.Duration(manifest.Spec.Template.Spec.TerminationGracePeriodSeconds) * time.Second
	required := managerGracefulShutdownTimeout + tracingShutdownTimeout
	if podGrace <= required {
		t.Fatalf("Pod termination grace = %s, must exceed the %s manager and tracing budget", podGrace, required)
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
	calls  int
}

func (syncer *fakeCacheSyncer) WaitForCacheSync(context.Context) bool {
	syncer.calls++
	return syncer.synced
}
