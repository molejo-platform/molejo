package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api/generated"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/observability"
)

type concurrencyLimiter struct {
	sync.Mutex
	active map[int64]int
}

func (l *concurrencyLimiter) acquire(actorID int64, maximum int) bool {
	l.Lock()
	defer l.Unlock()
	if l.active[actorID] >= maximum {
		return false
	}
	l.active[actorID]++
	return true
}

func (l *concurrencyLimiter) release(actorID int64) {
	l.Lock()
	defer l.Unlock()
	if l.active[actorID] <= 1 {
		delete(l.active, actorID)
		return
	}
	l.active[actorID]--
}

func (h *generatedHandler) ListAppEnvironmentRuntimeLogs(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.ListAppEnvironmentRuntimeLogsParams) {
	_, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	from, to, ok := observabilityRange(w, r, params.From, params.To, h.server.Config.ObservabilityMaxWindow)
	if !ok {
		return
	}
	query := observability.LogQuery{From: from, To: to, Limit: 200}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	if params.Search != nil {
		query.Search = strings.TrimSpace(*params.Search)
	}
	if params.Instance != nil {
		query.Instance = strings.TrimSpace(*params.Instance)
	}
	items, err := h.server.Observability.Logs(r.Context(), runtimeScope(workspace, appEnvironment), query)
	if err != nil {
		h.writeObservabilityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, observabilityLogsResponse{From: from, To: to, Items: items})
}

func (h *generatedHandler) GetAppEnvironmentRuntimeMetrics(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.GetAppEnvironmentRuntimeMetricsParams) {
	_, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	from, to, ok := observabilityRange(w, r, params.From, params.To, h.server.Config.ObservabilityMaxWindow)
	if !ok {
		return
	}
	step := time.Minute
	if params.StepSeconds != nil {
		step = time.Duration(*params.StepSeconds) * time.Second
	}
	metrics, err := h.server.Observability.Metrics(r.Context(), runtimeScope(workspace, appEnvironment), observability.MetricQuery{From: from, To: to, Step: step})
	if err != nil {
		h.writeObservabilityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (h *generatedHandler) ListAppEnvironmentRuntimeEvents(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.ListAppEnvironmentRuntimeEventsParams) {
	_, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	from, to, ok := observabilityRange(w, r, params.From, params.To, h.server.Config.ObservabilityMaxWindow)
	if !ok {
		return
	}
	limit := 100
	if params.Limit != nil {
		limit = *params.Limit
	}
	operations, err := h.server.Store.ListAppEnvironmentOperations(r.Context(), workspace.ID, appEnvironment.ID, from, to, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "runtime events could not be read", r)
		return
	}
	items := operationEvents(operations)
	kubernetesEvents, backendErr := h.server.Observability.Events(r.Context(), runtimeScope(workspace, appEnvironment), observability.EventQuery{From: from, To: to, Limit: limit})
	if backendErr != nil {
		h.server.logger().Warn("runtime event backend unavailable", "request_id", requestID(r), "app_environment_id", appEnvironment.PublicID)
	} else {
		items = append(items, kubernetesEvents...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Timestamp.After(items[j].Timestamp) })
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, observabilityEventsResponse{From: from, To: to, Items: items})
}

func (h *generatedHandler) StreamAppEnvironmentRuntimeLogs(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.StreamAppEnvironmentRuntimeLogsParams) {
	actor, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	if !h.server.logLiveLimiter.acquire(actor.ID, h.server.Config.ObservabilityLivePerUser) {
		writeError(w, http.StatusTooManyRequests, "live_stream_limit", "too many live log streams", r)
		return
	}
	defer h.server.logLiveLimiter.release(actor.ID)
	search, instance := "", ""
	if params.Search != nil {
		search = strings.TrimSpace(*params.Search)
	}
	if params.Instance != nil {
		instance = strings.TrimSpace(*params.Instance)
	}
	start := time.Now().UTC().Add(-2 * h.server.Config.ObservabilityLivePoll)
	initial, err := h.server.Observability.Logs(r.Context(), runtimeScope(workspace, appEnvironment), observability.LogQuery{From: start, To: time.Now().UTC(), Search: search, Instance: instance, Limit: 200})
	if err != nil {
		h.writeObservabilityError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "stream_unsupported", "live streaming is unavailable", r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if !writeSSE(w, flusher, "retry: 5000\n\n") {
		return
	}
	seen := map[string]struct{}{}
	if !writeLiveLogs(w, flusher, initial, seen) {
		return
	}
	ticker := time.NewTicker(h.server.Config.ObservabilityLivePoll)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	reauthorize := time.NewTicker(30 * time.Second)
	defer reauthorize.Stop()
	timeout := time.NewTimer(h.server.Config.ObservabilityLiveTTL)
	defer timeout.Stop()
	cursor := time.Now().UTC()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			_ = writeSSE(w, flusher, "event: end\ndata: {\"reason\":\"stream_ttl\"}\n\n")
			return
		case <-heartbeat.C:
			if !writeSSE(w, flusher, ": heartbeat\n\n") {
				return
			}
		case <-reauthorize.C:
			currentActor, _, authenticated := h.server.session(r)
			currentWorkspace, authErr := h.server.Store.WorkspaceForActor(r.Context(), currentActor)
			if !authenticated || currentActor != actor.ID || authErr != nil || currentWorkspace.ID != workspace.ID {
				_ = writeSSE(w, flusher, "event: end\ndata: {\"reason\":\"authorization_changed\"}\n\n")
				return
			}
		case now := <-ticker.C:
			items, queryErr := h.server.Observability.Logs(r.Context(), runtimeScope(workspace, appEnvironment), observability.LogQuery{From: cursor.Add(-time.Second), To: now.UTC(), Search: search, Instance: instance, Limit: 200})
			if queryErr != nil {
				_ = writeSSE(w, flusher, "event: telemetry-error\ndata: {\"code\":\"observability_unavailable\"}\n\n")
				return
			}
			if !writeLiveLogs(w, flusher, items, seen) {
				return
			}
			cursor = now.UTC()
			if len(seen) > 2000 {
				seen = map[string]struct{}{}
			}
		}
	}
}

func (h *generatedHandler) StreamAppEnvironmentRuntimeMetrics(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) {
	actor, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	if !h.server.metricsLiveLimiter.acquire(actor.ID, h.server.Config.ObservabilityMetricsLivePerUser) {
		writeError(w, http.StatusTooManyRequests, "metrics_stream_limit", "too many live metric streams", r)
		return
	}
	defer h.server.metricsLiveLimiter.release(actor.ID)

	snapshot, err := h.server.Observability.CurrentMetrics(r.Context(), runtimeScope(workspace, appEnvironment), time.Now().UTC())
	if err != nil {
		h.writeObservabilityError(w, r, err)
		return
	}
	flusher, supported := w.(http.Flusher)
	if !supported {
		writeError(w, http.StatusNotImplemented, "stream_unsupported", "live streaming is unavailable", r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if !writeSSE(w, flusher, "retry: 5000\n\n") {
		return
	}
	if !writeMetricSnapshot(w, flusher, snapshot) {
		return
	}

	poll := time.NewTicker(h.server.Config.ObservabilityMetricsLivePoll)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	reauthorize := time.NewTicker(30 * time.Second)
	defer reauthorize.Stop()
	timeout := time.NewTimer(h.server.Config.ObservabilityLiveTTL)
	defer timeout.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			_ = writeSSE(w, flusher, "event: end\ndata: {\"reason\":\"stream_ttl\"}\n\n")
			return
		case <-heartbeat.C:
			if !writeSSE(w, flusher, ": heartbeat\n\n") {
				return
			}
		case <-reauthorize.C:
			currentActor, _, authenticated := h.server.session(r)
			currentWorkspace, authErr := h.server.Store.WorkspaceForActor(r.Context(), currentActor)
			if !authenticated || currentActor != actor.ID || authErr != nil || currentWorkspace.ID != workspace.ID {
				_ = writeSSE(w, flusher, "event: end\ndata: {\"reason\":\"authorization_changed\"}\n\n")
				return
			}
		case now := <-poll.C:
			next, queryErr := h.server.Observability.CurrentMetrics(r.Context(), runtimeScope(workspace, appEnvironment), now.UTC())
			if queryErr != nil {
				_ = writeSSE(w, flusher, "event: telemetry-error\ndata: {\"code\":\"observability_unavailable\"}\n\n")
				return
			}
			if !writeMetricSnapshot(w, flusher, next) {
				return
			}
		}
	}
}

type observabilityLogsResponse struct {
	From  time.Time                `json:"from"`
	To    time.Time                `json:"to"`
	Items []observability.LogEntry `json:"items"`
}

type observabilityEventsResponse struct {
	From  time.Time             `json:"from"`
	To    time.Time             `json:"to"`
	Items []observability.Event `json:"items"`
}

func (h *generatedHandler) observabilityScope(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) (domain.Actor, domain.Workspace, domain.AppEnvironment, bool) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return domain.Actor{}, domain.Workspace{}, domain.AppEnvironment{}, false
	}
	appEnvironment, ok := h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	return actor, workspace, appEnvironment, ok
}

func runtimeScope(workspace domain.Workspace, appEnvironment domain.AppEnvironment) observability.Scope {
	return observability.Scope{Namespace: workspace.Namespace, RuntimeName: appEnvironment.RuntimeName}
}

func observabilityRange(w http.ResponseWriter, r *http.Request, fromParam, toParam *time.Time, maximum time.Duration) (time.Time, time.Time, bool) {
	to := time.Now().UTC()
	if toParam != nil {
		to = toParam.UTC()
	}
	from := to.Add(-15 * time.Minute)
	if fromParam != nil {
		from = fromParam.UTC()
	}
	if !from.Before(to) || to.Sub(from) > maximum || to.After(time.Now().UTC().Add(time.Minute)) {
		writeError(w, http.StatusBadRequest, "observability_range_invalid", "time range is invalid or exceeds the allowed window", r)
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func operationEvents(operations []domain.Operation) []observability.Event {
	items := make([]observability.Event, 0, len(operations))
	for _, operation := range operations {
		message := "operation " + strings.ToLower(operation.Status)
		if operation.ErrorMessage != "" {
			message = operation.ErrorMessage
		}
		items = append(items, observability.Event{Timestamp: operation.UpdatedAt, Source: "control-plane", Type: operation.Kind, Reason: operation.Status, Message: message})
	}
	return items
}

func writeLiveLogs(w http.ResponseWriter, flusher http.Flusher, items []observability.LogEntry, seen map[string]struct{}) bool {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Timestamp.Before(items[j].Timestamp) })
	for _, item := range items {
		key := item.Timestamp.Format(time.RFC3339Nano) + "\x00" + item.Instance + "\x00" + item.Body
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		payload, err := json.Marshal(item)
		if err != nil {
			continue
		}
		if !writeSSE(w, flusher, fmt.Sprintf("event: log\ndata: %s\n\n", payload)) {
			return false
		}
	}
	return true
}

func writeMetricSnapshot(w http.ResponseWriter, flusher http.Flusher, snapshot observability.MetricSnapshot) bool {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return false
	}
	return writeSSE(w, flusher, fmt.Sprintf("id: %d\nevent: metrics\ndata: %s\n\n", snapshot.ObservedAt.UnixMilli(), payload))
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, payload string) bool {
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	if _, err := fmt.Fprint(w, payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func (h *generatedHandler) writeObservabilityError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, observability.ErrUnavailable) {
		h.server.logger().Warn("observability backend unavailable", "request_id", requestID(r))
		writeError(w, http.StatusServiceUnavailable, "observability_unavailable", "runtime observability is temporarily unavailable", r)
		return
	}
	writeError(w, http.StatusInternalServerError, "observability_failed", "runtime observability could not be read", r)
}
