package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/observability"
)

func TestMetricSnapshotCacheCoalescesConcurrentReaders(t *testing.T) {
	cache := newMetricSnapshotCache(30 * time.Second)
	scope := observability.Scope{Namespace: "workspace-a", RuntimeName: "runtime-a"}
	at := time.Now().UTC()
	var calls atomic.Int32
	load := func() (observability.MetricSnapshot, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return observability.MetricSnapshot{ObservedAt: at}, nil
	}

	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := cache.get(context.Background(), scope, at, load); err != nil {
				t.Errorf("get failed: %v", err)
			}
		}()
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("backend called %d times, want 1", calls.Load())
	}
}
