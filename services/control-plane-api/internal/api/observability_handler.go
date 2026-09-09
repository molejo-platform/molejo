package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
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
	from, to, ok := observabilityRange(w, r, params.From, params.To, h.server.config.ObservabilityLogMaxWindow)
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
	if params.Cursor != nil {
		snapshot, before, err := decodeHistoricalLogCursor(*params.Cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "observability_cursor_invalid", "log cursor is invalid", r)
			return
		}
		query.Snapshot, query.Before = snapshot, &before
	} else {
		watermark, err := h.server.historicalLogs.LogWatermark(r.Context())
		if err != nil {
			h.writeObservabilityError(w, r, err)
			return
		}
		query.Snapshot = watermark
	}
	page, err := h.server.historicalLogs.Logs(r.Context(), runtimeScope(workspace, appEnvironment), query)
	if err != nil {
		h.writeObservabilityError(w, r, err)
		return
	}
	var nextCursor *string
	if page.Next != nil {
		encoded := encodeHistoricalLogCursor(page.LiveCursor, *page.Next)
		nextCursor = &encoded
	}
	writeJSON(w, http.StatusOK, observabilityLogsResponse{From: from, To: to, LiveCursor: encodeLiveLogCursor(page.LiveCursor), NextCursor: nextCursor, Items: page.Items})
}

func (h *generatedHandler) GetAppEnvironmentRuntimeMetrics(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.GetAppEnvironmentRuntimeMetricsParams) {
	_, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	from, to, ok := observabilityRange(w, r, params.From, params.To, h.server.config.ObservabilityMetricMaxWindow)
	if !ok {
		return
	}
	step := observabilityMetricStep(to.Sub(from))
	metrics, err := h.server.historicalMetrics.Metrics(r.Context(), runtimeScope(workspace, appEnvironment), observability.MetricQuery{From: from, To: to, Step: step})
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
	from, to, ok := observabilityRange(w, r, params.From, params.To, h.server.config.ObservabilityEventMaxWindow)
	if !ok {
		return
	}
	limit := 100
	if params.Limit != nil {
		limit = *params.Limit
	}
	operations, err := h.server.store.ListAppEnvironmentOperations(r.Context(), workspace.ID, appEnvironment.ID, from, to, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "runtime events could not be read", r)
		return
	}
	items := operationEvents(operations)
	kubernetesEvents, partial, unavailable, backendErr := h.server.currentEvents.CurrentEvents(r.Context(), runtimeScope(workspace, appEnvironment), observability.EventQuery{From: from, To: to, Limit: limit})
	if backendErr != nil {
		h.server.logger().Warn("runtime event backend unavailable", "request_id", requestID(r), "app_environment_id", appEnvironment.PublicID)
		partial, unavailable = true, []string{"kubernetes"}
	} else {
		items = append(items, kubernetesEvents...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Timestamp.After(items[j].Timestamp) })
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, observabilityEventsResponse{From: from, To: to, Partial: partial, Unavailable: unavailable, Items: items})
}

func (h *generatedHandler) StreamAppEnvironmentRuntimeLogs(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.StreamAppEnvironmentRuntimeLogsParams) {
	actor, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	if !h.server.logLiveLimiter.acquire(actor.ID, h.server.config.ObservabilityLivePerUser) {
		writeError(w, http.StatusTooManyRequests, "live_stream_limit", "too many live log streams", r)
		return
	}
	defer h.server.logLiveLimiter.release(actor.ID)
	search := ""
	if params.Search != nil {
		search = strings.TrimSpace(*params.Search)
	}
	encodedCursor := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if params.Cursor != nil && encodedCursor == "" {
		encodedCursor = strings.TrimSpace(*params.Cursor)
	}
	var cursor observability.LogCursor
	var err error
	if encodedCursor == "" {
		cursor = observability.LogCursor{IngestedAt: time.Now().UTC().Add(-5 * time.Minute), ID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}
	} else {
		cursor, err = decodeLiveLogCursor(encodedCursor)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "observability_cursor_invalid", "live log cursor is invalid", r)
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
	scope := runtimeScope(workspace, appEnvironment)
	if next, healthy := h.drainLiveLogs(r.Context(), w, flusher, scope, cursor, search); !healthy {
		return
	} else {
		cursor = next
	}
	ticker := time.NewTicker(h.server.config.ObservabilityLivePoll)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	reauthorize := time.NewTicker(30 * time.Second)
	defer reauthorize.Stop()
	timeout := time.NewTimer(h.server.config.ObservabilityLiveTTL)
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
			currentWorkspace, authErr := h.server.store.FindWorkspaceForUser(r.Context(), currentActor, workspace.PublicID)
			if !authenticated || currentActor != actor.ID || authErr != nil || currentWorkspace.ID != workspace.ID {
				_ = writeSSE(w, flusher, "event: end\ndata: {\"reason\":\"authorization_changed\"}\n\n")
				return
			}
		case <-ticker.C:
			next, healthy := h.drainLiveLogs(r.Context(), w, flusher, scope, cursor, search)
			if !healthy {
				return
			}
			cursor = next
		}
	}
}

func (h *generatedHandler) drainLiveLogs(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, scope observability.Scope, cursor observability.LogCursor, search string) (observability.LogCursor, bool) {
	const (
		batchSize    = 500
		maximumPages = 20
	)
	for page := 0; page < maximumPages; page++ {
		batch, err := h.server.currentLogs.CurrentLogs(ctx, scope, cursor.IngestedAt.Add(time.Nanosecond), search, batchSize)
		if err != nil {
			_ = writeSSE(w, flusher, "event: telemetry-error\ndata: {\"code\":\"observability_unavailable\"}\n\n")
			return cursor, false
		}
		if len(batch.Items) == 0 {
			return cursor, true
		}
		cursor = batch.Cursor
		if !writeLiveLogBatch(w, flusher, encodeLiveLogCursor(cursor), batch.Items) {
			return cursor, false
		}
		if len(batch.Items) < batchSize {
			return cursor, true
		}
	}
	return cursor, true
}

func (h *generatedHandler) StreamAppEnvironmentRuntimeMetrics(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) {
	actor, workspace, appEnvironment, ok := h.observabilityScope(w, r, workspaceID, projectID, appID, appEnvironmentID)
	if !ok {
		return
	}
	if !h.server.metricsLiveLimiter.acquire(actor.ID, h.server.config.ObservabilityMetricsLivePerUser) {
		writeError(w, http.StatusTooManyRequests, "metrics_stream_limit", "too many live metric streams", r)
		return
	}
	defer h.server.metricsLiveLimiter.release(actor.ID)

	scope := runtimeScope(workspace, appEnvironment)
	snapshot, err := h.server.currentMetrics(r.Context(), scope, time.Now().UTC())
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

	poll := time.NewTicker(h.server.config.ObservabilityMetricsLivePoll)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	reauthorize := time.NewTicker(30 * time.Second)
	defer reauthorize.Stop()
	timeout := time.NewTimer(h.server.config.ObservabilityLiveTTL)
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
			currentWorkspace, authErr := h.server.store.FindWorkspaceForUser(r.Context(), currentActor, workspace.PublicID)
			if !authenticated || currentActor != actor.ID || authErr != nil || currentWorkspace.ID != workspace.ID {
				_ = writeSSE(w, flusher, "event: end\ndata: {\"reason\":\"authorization_changed\"}\n\n")
				return
			}
		case now := <-poll.C:
			next, queryErr := h.server.currentMetrics(r.Context(), scope, now.UTC())
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

func observabilityMetricStep(window time.Duration) time.Duration {
	for _, step := range []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour} {
		if window/step < 1_000 {
			return step
		}
	}
	return time.Hour
}

type observabilityLogsResponse struct {
	From       time.Time                `json:"from"`
	To         time.Time                `json:"to"`
	LiveCursor string                   `json:"liveCursor"`
	NextCursor *string                  `json:"nextCursor"`
	Items      []observability.LogEntry `json:"items"`
}

type observabilityEventsResponse struct {
	From        time.Time             `json:"from"`
	To          time.Time             `json:"to"`
	Partial     bool                  `json:"partial"`
	Unavailable []string              `json:"unavailable"`
	Items       []observability.Event `json:"items"`
}

func (h *generatedHandler) observabilityScope(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) (identity.User, domain.Workspace, domain.AppEnvironment, bool) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return identity.User{}, domain.Workspace{}, domain.AppEnvironment{}, false
	}
	appEnvironment, ok := h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	return actor, workspace, appEnvironment, ok
}

func runtimeScope(workspace domain.Workspace, appEnvironment domain.AppEnvironment) observability.Scope {
	return observability.Scope{
		ClusterID:        appEnvironment.ClusterPublicID,
		ClusterUID:       appEnvironment.ClusterUID,
		WorkspaceID:      workspace.PublicID,
		AppEnvironmentID: appEnvironment.PublicID,
		Namespace:        workspace.Namespace,
		RuntimeName:      appEnvironment.RuntimeName,
	}
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

func writeLiveLogBatch(w http.ResponseWriter, flusher http.Flusher, cursor string, items []observability.LogEntry) bool {
	payload, err := json.Marshal(struct {
		Cursor string                   `json:"cursor"`
		Items  []observability.LogEntry `json:"items"`
	}{Cursor: cursor, Items: items})
	if err != nil {
		return false
	}
	return writeSSE(w, flusher, fmt.Sprintf("id: %s\nevent: logs\ndata: %s\n\n", cursor, payload))
}

type encodedLogCursor struct {
	Version    int    `json:"v"`
	IngestedAt string `json:"ingestedAt"`
	IngestedID string `json:"ingestedId"`
	BeforeAt   string `json:"beforeAt,omitempty"`
	BeforeID   string `json:"beforeId,omitempty"`
}

func encodeLiveLogCursor(cursor observability.LogCursor) string {
	return encodeLogCursor(encodedLogCursor{Version: 1, IngestedAt: cursor.IngestedAt.UTC().Format(time.RFC3339Nano), IngestedID: cursor.ID})
}

func encodeHistoricalLogCursor(snapshot observability.LogCursor, before observability.LogPosition) string {
	return encodeLogCursor(encodedLogCursor{Version: 1, IngestedAt: snapshot.IngestedAt.UTC().Format(time.RFC3339Nano), IngestedID: snapshot.ID, BeforeAt: before.Timestamp.UTC().Format(time.RFC3339Nano), BeforeID: before.ID})
}

func encodeLogCursor(cursor encodedLogCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeLiveLogCursor(encoded string) (observability.LogCursor, error) {
	cursor, err := decodeLogCursor(encoded)
	if err != nil {
		return observability.LogCursor{}, err
	}
	ingestedAt, err := time.Parse(time.RFC3339Nano, cursor.IngestedAt)
	result := observability.LogCursor{IngestedAt: ingestedAt, ID: cursor.IngestedID}
	if err != nil || !result.Valid() {
		return observability.LogCursor{}, errors.New("invalid live log cursor")
	}
	return result, nil
}

func decodeHistoricalLogCursor(encoded string) (observability.LogCursor, observability.LogPosition, error) {
	cursor, err := decodeLogCursor(encoded)
	if err != nil {
		return observability.LogCursor{}, observability.LogPosition{}, err
	}
	ingestedAt, ingestedErr := time.Parse(time.RFC3339Nano, cursor.IngestedAt)
	beforeAt, beforeErr := time.Parse(time.RFC3339Nano, cursor.BeforeAt)
	snapshot := observability.LogCursor{IngestedAt: ingestedAt, ID: cursor.IngestedID}
	before := observability.LogPosition{Timestamp: beforeAt, ID: cursor.BeforeID}
	if ingestedErr != nil || beforeErr != nil || !snapshot.Valid() || !before.Valid() {
		return observability.LogCursor{}, observability.LogPosition{}, errors.New("invalid historical log cursor")
	}
	return snapshot, before, nil
}

func decodeLogCursor(encoded string) (encodedLogCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(payload) > 1024 {
		return encodedLogCursor{}, errors.New("invalid log cursor")
	}
	var cursor encodedLogCursor
	if json.Unmarshal(payload, &cursor) != nil || cursor.Version != 1 {
		return encodedLogCursor{}, errors.New("invalid log cursor")
	}
	return cursor, nil
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
