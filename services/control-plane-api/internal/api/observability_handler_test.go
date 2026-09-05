package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
)

func TestObservabilityMetricStepBoundsPointCount(t *testing.T) {
	tests := []struct {
		window time.Duration
		want   time.Duration
	}{
		{15 * time.Minute, 15 * time.Second},
		{24 * time.Hour, 5 * time.Minute},
		{7 * 24 * time.Hour, 15 * time.Minute},
		{30 * 24 * time.Hour, time.Hour},
	}
	for _, test := range tests {
		if got := observabilityMetricStep(test.window); got != test.want {
			t.Errorf("window %s: got step %s, want %s", test.window, got, test.want)
		}
		if points := int(test.window/test.want) + 1; points > 1_000 {
			t.Errorf("window %s produces %d points", test.window, points)
		}
	}
}

type recordingObservabilityReader struct {
	scope observability.Scope
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (r *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.deadlines = append(r.deadlines, deadline)
	return nil
}

func (r *recordingObservabilityReader) LogWatermark(context.Context) (observability.LogCursor, error) {
	return observability.LogCursor{IngestedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), ID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}, nil
}

func (r *recordingObservabilityReader) Logs(_ context.Context, scope observability.Scope, query observability.LogQuery) (observability.LogPage, error) {
	r.scope = scope
	return observability.LogPage{Items: []observability.LogEntry{}, LiveCursor: query.Snapshot}, nil
}

func (r *recordingObservabilityReader) LiveLogs(_ context.Context, scope observability.Scope, query observability.LiveLogQuery) (observability.LogBatch, error) {
	r.scope = scope
	return observability.LogBatch{Items: []observability.LogEntry{}, Cursor: query.After}, nil
}

func (r *recordingObservabilityReader) Metrics(_ context.Context, scope observability.Scope, query observability.MetricQuery) (observability.Metrics, error) {
	r.scope = scope
	return observability.Metrics{From: query.From, To: query.To, Step: query.Step.String(), Series: []observability.MetricSeries{}}, nil
}

func (r *recordingObservabilityReader) CurrentMetrics(_ context.Context, scope observability.Scope, at time.Time) (observability.MetricSnapshot, error) {
	r.scope = scope
	return observability.MetricSnapshot{ObservedAt: at, Samples: []observability.MetricSample{{Name: "available", Unit: "replicas", Timestamp: at, Value: 1}}}, nil
}

func (r *recordingObservabilityReader) Events(context.Context, observability.Scope, observability.EventQuery) ([]observability.Event, error) {
	return []observability.Event{}, nil
}

func TestMetricSnapshotClearsTheWriteDeadlineAfterFlushing(t *testing.T) {
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	snapshot := observability.MetricSnapshot{ObservedAt: time.Now().UTC()}

	if !writeMetricSnapshot(recorder, recorder, snapshot) {
		t.Fatal("metric snapshot write failed")
	}
	if len(recorder.deadlines) != 2 || recorder.deadlines[0].IsZero() || !recorder.deadlines[1].IsZero() {
		t.Fatalf("write deadlines were not bounded and cleared: %#v", recorder.deadlines)
	}
}

func TestLiveLogBatchUsesStableCursorAndPreservesIdenticalRecords(t *testing.T) {
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	items := []observability.LogEntry{
		{ID: "log-a", Timestamp: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), Body: "same", Severity: "INFO"},
		{ID: "log-b", Timestamp: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), Body: "same", Severity: "INFO"},
	}

	if !writeLiveLogBatch(recorder, recorder, "cursor-2", items) {
		t.Fatal("live log batch write failed")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "id: cursor-2") || !strings.Contains(body, "event: logs") || strings.Count(body, `"body":"same"`) != 2 {
		t.Fatalf("unexpected SSE batch: %s", body)
	}
}

func TestLogCursorsRoundTripWithoutExposingStorageFields(t *testing.T) {
	snapshot := observability.LogCursor{IngestedAt: time.Date(2026, 8, 28, 12, 0, 0, 123, time.UTC), ID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}
	position := observability.LogPosition{Timestamp: time.Date(2026, 8, 28, 11, 59, 0, 456, time.UTC), ID: "11111111-1111-4111-8111-111111111111"}
	encoded := encodeHistoricalLogCursor(snapshot, position)
	decodedSnapshot, decodedPosition, err := decodeHistoricalLogCursor(encoded)
	if err != nil || decodedSnapshot != snapshot || decodedPosition != position {
		t.Fatalf("decoded snapshot=%+v position=%+v err=%v", decodedSnapshot, decodedPosition, err)
	}
	if strings.Contains(encoded, snapshot.ID) || strings.Contains(encoded, position.ID) {
		t.Fatalf("cursor exposes storage fields: %s", encoded)
	}
	if _, err := decodeLiveLogCursor("not-a-cursor"); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}

func TestObservabilityRangeRejectsUnboundedQueries(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	to := time.Now().UTC()
	from := to.Add(-25 * time.Hour)
	_, _, ok := observabilityRange(response, request, &from, &to, 24*time.Hour)
	if ok || response.Code != http.StatusBadRequest {
		t.Fatalf("ok=%t status=%d", ok, response.Code)
	}
}

func TestConcurrencyLimiterIsPerActor(t *testing.T) {
	t.Parallel()

	limiter := &concurrencyLimiter{active: map[int64]int{}}
	if !limiter.acquire(1, 1) || limiter.acquire(1, 1) || !limiter.acquire(2, 1) {
		t.Fatal("unexpected limiter decision")
	}
	limiter.release(1)
	if !limiter.acquire(1, 1) {
		t.Fatal("released capacity was not restored")
	}
}

func TestOperationEventsNeverExposeInternalFields(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	items := operationEvents([]domain.Operation{{PublicID: "op-private", WorkerID: "worker-private", Kind: domain.OperationApplyDeployment, Status: domain.OperationFailed, ErrorMessage: "runtime failed", UpdatedAt: now}})
	if len(items) != 1 || items[0].Message != "runtime failed" || items[0].Reason != domain.OperationFailed {
		t.Fatalf("unexpected events: %#v", items)
	}
}

func TestObservabilityAPIResolvesScopeOnlyAfterFullAncestryAuthorization(t *testing.T) {
	storage, workspace, server, owner := newHierarchyAPITestFixture(t)
	reader := &recordingObservabilityReader{}
	server.observability = reader

	projectResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Observability"}`, nil)
	var project domain.Project
	decodeResponse(t, projectResponse, &project)
	environmentResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/environments", `{"name":"Production"}`, nil)
	var environment domain.Environment
	decodeResponse(t, environmentResponse, &environment)
	appResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/apps", `{"name":"API"}`, nil)
	var app domain.App
	decodeResponse(t, appResponse, &app)
	configuration := fmt.Sprintf(`{"environmentId":%q,"clusterId":%q,"branch":"main","workloadKind":"Stateless","configuration":{"replicas":1,"ports":[{"name":"http","containerPort":8080,"protocol":"TCP"}],"resources":{"requests":{"cpuMillis":50,"memoryMiB":64},"limits":{"cpuMillis":250,"memoryMiB":128}},"probes":{"startup":{"type":"HTTP","portName":"http","path":"/readyz"},"liveness":{"type":"HTTP","portName":"http","path":"/healthz"},"readiness":{"type":"HTTP","portName":"http","path":"/readyz"}},"publicEndpoints":[],"variables":[],"parameters":[]}}`, environment.PublicID, testAgentInstallationID)
	appEnvironmentResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/apps/"+app.PublicID+"/environments", configuration, nil)
	var appEnvironment domain.AppEnvironment
	decodeResponse(t, appEnvironmentResponse, &appEnvironment)
	persisted, err := storage.FindAppEnvironment(context.Background(), workspace.ID, appEnvironment.PublicID)
	if err != nil {
		t.Fatal(err)
	}

	base := "/api/v1/workspaces/" + workspace.PublicID + "/projects/" + project.PublicID + "/apps/" + app.PublicID + "/environments/" + appEnvironment.PublicID + "/observability"
	response := hierarchyRequest(t, server, owner, http.MethodGet, base+"/metrics", "", nil)
	if response.Code != http.StatusOK || reader.scope.Namespace != workspace.Namespace || reader.scope.RuntimeName != persisted.RuntimeName {
		t.Fatalf("status=%d scope=%+v body=%s", response.Code, reader.scope, response.Body.String())
	}
	server.config.ObservabilityLiveTTL = time.Millisecond
	server.config.ObservabilityMetricsLivePoll = time.Hour
	response = hierarchyRequest(t, server, owner, http.MethodGet, base+"/metrics/live", "", nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(response.Body.String(), "event: metrics") || !strings.Contains(response.Body.String(), `"name":"available"`) {
		t.Fatalf("live metrics status=%d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}

	otherAppResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/apps", `{"name":"Other"}`, nil)
	var otherApp domain.App
	decodeResponse(t, otherAppResponse, &otherApp)
	response = hierarchyRequest(t, server, owner, http.MethodGet, strings.Replace(base, "/apps/"+app.PublicID+"/", "/apps/"+otherApp.PublicID+"/", 1)+"/logs", "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-App scope returned status=%d body=%s", response.Code, response.Body.String())
	}
}
