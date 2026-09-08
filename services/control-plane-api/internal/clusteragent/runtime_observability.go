package clusteragent

import (
	"context"
	"errors"
	"strings"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
)

const runtimeQueryTimeout = 10 * time.Second

type RuntimeObservability struct {
	broker *RuntimeQueryBroker
}

func NewRuntimeObservability(broker *RuntimeQueryBroker) *RuntimeObservability {
	return &RuntimeObservability{broker: broker}
}

func (r *RuntimeObservability) CurrentLogs(ctx context.Context, scope observability.Scope, since time.Time, search string, limit int) (observability.LogBatch, error) {
	request := &clusteragentv1alpha1.RuntimeQueryRequest{DeadlineUnixMilli: time.Now().Add(runtimeQueryTimeout).UnixMilli(), AppEnvironmentId: scope.AppEnvironmentID, Query: &clusteragentv1alpha1.RuntimeQueryRequest_PodLogs{PodLogs: &clusteragentv1alpha1.PodLogsQuery{Namespace: scope.Namespace, RuntimeName: scope.RuntimeName, Container: "app", SinceUnixNano: since.UnixNano(), TailLines: int32(min(max(limit, 1), 2_000)), LimitBytes: 1 << 20}}}
	response, err := r.broker.Query(ctx, scope.ClusterID, request)
	if err != nil {
		return observability.LogBatch{}, runtimeObservabilityError(err)
	}
	items := []observability.LogEntry{}
	cursor := observability.LogCursor{IngestedAt: since, ID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}
	for _, chunk := range response.Chunks {
		result := chunk.GetPodLogs()
		if result == nil {
			continue
		}
		for _, item := range result.GetItems() {
			body := item.GetBody()
			if search != "" && !strings.Contains(strings.ToLower(body), strings.ToLower(search)) {
				continue
			}
			timestamp := time.Unix(0, item.GetTimestampUnixNano()).UTC()
			items = append(items, observability.LogEntry{ID: item.GetId(), Timestamp: timestamp, Body: body, Severity: item.GetSeverity()})
			if timestamp.After(cursor.IngestedAt) {
				cursor.IngestedAt = timestamp
			}
		}
	}
	return observability.LogBatch{Items: items, Cursor: cursor}, nil
}

func (r *RuntimeObservability) CurrentMetrics(ctx context.Context, scope observability.Scope, _ time.Time) (observability.MetricSnapshot, error) {
	request := &clusteragentv1alpha1.RuntimeQueryRequest{DeadlineUnixMilli: time.Now().Add(runtimeQueryTimeout).UnixMilli(), AppEnvironmentId: scope.AppEnvironmentID, Query: &clusteragentv1alpha1.RuntimeQueryRequest_CurrentPodMetrics{CurrentPodMetrics: &clusteragentv1alpha1.CurrentPodMetricsQuery{Namespace: scope.Namespace, RuntimeName: scope.RuntimeName}}}
	response, err := r.broker.Query(ctx, scope.ClusterID, request)
	if err != nil {
		return observability.MetricSnapshot{}, runtimeObservabilityError(err)
	}
	result := firstMetrics(response.Chunks)
	if result == nil {
		return observability.MetricSnapshot{}, observability.ErrUnavailable
	}
	snapshot := observability.MetricSnapshot{ObservedAt: time.Unix(0, result.GetObservedAtUnixNano()).UTC(), Partial: result.GetPartial(), Unavailable: append([]string{}, result.GetUnavailable()...), Samples: []observability.MetricSample{}}
	for _, item := range result.GetSamples() {
		snapshot.Samples = append(snapshot.Samples, observability.MetricSample{Name: item.GetName(), Unit: item.GetUnit(), Timestamp: time.Unix(0, item.GetTimestampUnixNano()).UTC(), Value: item.GetValue()})
	}
	return snapshot, nil
}

func (r *RuntimeObservability) CurrentEvents(ctx context.Context, scope observability.Scope, query observability.EventQuery) ([]observability.Event, bool, []string, error) {
	request := &clusteragentv1alpha1.RuntimeQueryRequest{DeadlineUnixMilli: time.Now().Add(runtimeQueryTimeout).UnixMilli(), AppEnvironmentId: scope.AppEnvironmentID, Query: &clusteragentv1alpha1.RuntimeQueryRequest_KubernetesEvents{KubernetesEvents: &clusteragentv1alpha1.KubernetesEventsQuery{Namespace: scope.Namespace, RuntimeName: scope.RuntimeName, SinceUnixNano: query.From.UnixNano(), Limit: int32(min(max(query.Limit, 1), 200))}}}
	response, err := r.broker.Query(ctx, scope.ClusterID, request)
	if err != nil {
		return nil, true, []string{"kubernetes"}, runtimeObservabilityError(err)
	}
	items := []observability.Event{}
	partial, unavailable := false, []string{}
	for _, chunk := range response.Chunks {
		result := chunk.GetKubernetesEvents()
		if result == nil {
			continue
		}
		partial = partial || result.GetPartial()
		if result.GetPartial() {
			unavailable = []string{"kubernetes"}
		}
		for _, item := range result.GetItems() {
			items = append(items, observability.Event{Timestamp: time.Unix(0, item.GetTimestampUnixNano()).UTC(), Source: "kubernetes", Type: item.GetType(), Reason: item.GetReason(), Message: item.GetMessage()})
		}
	}
	return items, partial, unavailable, nil
}

func firstMetrics(chunks []*clusteragentv1alpha1.RuntimeQueryChunk) *clusteragentv1alpha1.CurrentPodMetricsResult {
	for _, chunk := range chunks {
		if result := chunk.GetCurrentPodMetrics(); result != nil {
			return result
		}
	}
	return nil
}

func runtimeObservabilityError(err error) error {
	if errors.Is(err, ErrRuntimeQueryUnavailable) || errors.Is(err, ErrRuntimeQueryRejected) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return observability.ErrUnavailable
	}
	return err
}
