package api

import (
	"context"
	"sync"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
)

type metricSnapshotEntry struct {
	snapshot observability.MetricSnapshot
	expires  time.Time
	wait     chan struct{}
}

type metricSnapshotCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[observability.Scope]*metricSnapshotEntry
}

func newMetricSnapshotCache(ttl time.Duration) *metricSnapshotCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &metricSnapshotCache{ttl: ttl, entries: make(map[observability.Scope]*metricSnapshotEntry)}
}

func (s *Server) currentMetrics(ctx context.Context, scope observability.Scope, at time.Time) (observability.MetricSnapshot, error) {
	return s.metricSnapshots.get(ctx, scope, at, func() (observability.MetricSnapshot, error) {
		return s.currentObservability.CurrentMetrics(ctx, scope, at)
	})
}

func (c *metricSnapshotCache) get(ctx context.Context, scope observability.Scope, at time.Time, load func() (observability.MetricSnapshot, error)) (observability.MetricSnapshot, error) {
	for {
		c.mu.Lock()
		entry := c.entries[scope]
		if entry != nil && entry.wait == nil && at.Before(entry.expires) {
			snapshot := entry.snapshot
			c.mu.Unlock()
			return snapshot, nil
		}
		if entry != nil && entry.wait != nil {
			wait := entry.wait
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return observability.MetricSnapshot{}, ctx.Err()
			case <-wait:
				continue
			}
		}
		wait := make(chan struct{})
		c.entries[scope] = &metricSnapshotEntry{wait: wait}
		c.mu.Unlock()

		snapshot, err := load()
		c.mu.Lock()
		if err == nil {
			c.entries[scope] = &metricSnapshotEntry{snapshot: snapshot, expires: at.Add(c.ttl)}
		} else {
			delete(c.entries, scope)
		}
		close(wait)
		c.mu.Unlock()
		return snapshot, err
	}
}
