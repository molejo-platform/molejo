package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestIsCanceledReconciliation(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer deadlineCancel()
	<-deadlineCtx.Done()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "active context with cancellation error", ctx: context.Background(), err: context.Canceled},
		{name: "canceled context with cancellation error", ctx: canceledCtx, err: context.Canceled, want: true},
		{name: "canceled context with wrapped cancellation", ctx: canceledCtx, err: fmt.Errorf("read child: %w", context.Canceled), want: true},
		{name: "canceled context with unrelated error", ctx: canceledCtx, err: errors.New("forbidden")},
		{name: "deadline is not shutdown cancellation", ctx: deadlineCtx, err: context.DeadlineExceeded},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isCanceledReconciliation(test.ctx, test.err); got != test.want {
				t.Fatalf("isCanceledReconciliation() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestReconcilersTreatShutdownCancellationAsUnfinishedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceledErr := fmt.Errorf("read resource: %w", context.Canceled)
	request := ctrl.Request{}

	t.Run("AppDeployment", func(t *testing.T) {
		spanRecorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		reconciler := &AppDeploymentReconciler{
			Client: failingGetClient{err: canceledErr},
			Tracer: provider.Tracer(tracerName),
		}
		if _, err := reconciler.Reconcile(ctx, request); err != nil {
			t.Fatalf("Reconcile() error = %v, want clean cancellation", err)
		}
		spans := spanRecorder.Ended()
		if len(spans) != 2 {
			t.Fatalf("ended spans = %d, want 2", len(spans))
		}
		root := spans[1]
		if value, ok := spanAttribute(root.Attributes(), "fruto.reconciliation.outcome"); !ok || value != "canceled" {
			t.Fatalf("reconciliation outcome = %q found=%t, want canceled", value, ok)
		}
		if root.Status().Code != codes.Unset {
			t.Fatalf("span status = %v, want unset", root.Status().Code)
		}
	})

	t.Run("AppVolume", func(t *testing.T) {
		reconciler := &AppVolumeReconciler{Client: failingGetClient{err: canceledErr}}
		if _, err := reconciler.Reconcile(ctx, request); err != nil {
			t.Fatalf("Reconcile() error = %v, want clean cancellation", err)
		}
	})
}

func spanAttribute(attributes []attribute.KeyValue, key string) (string, bool) {
	for _, item := range attributes {
		if string(item.Key) == key {
			return item.Value.AsString(), true
		}
	}
	return "", false
}
