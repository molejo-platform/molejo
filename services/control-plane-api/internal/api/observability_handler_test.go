package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/observability"
)

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

func (r *recordingObservabilityReader) Logs(_ context.Context, scope observability.Scope, _ observability.LogQuery) ([]observability.LogEntry, error) {
	r.scope = scope
	return []observability.LogEntry{}, nil
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
	server.Observability = reader

	projectResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Observability"}`, nil)
	var project domain.Project
	decodeResponse(t, projectResponse, &project)
	environmentResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/environments", `{"name":"Production"}`, nil)
	var environment domain.Environment
	decodeResponse(t, environmentResponse, &environment)
	appResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/apps", `{"name":"API"}`, nil)
	var app domain.App
	decodeResponse(t, appResponse, &app)
	configuration := fmt.Sprintf(`{"environmentId":%q,"branch":"main","configuration":{"replicas":1,"port":8080,"resources":{"requests":{"cpuMillis":50,"memoryMiB":64},"limits":{"cpuMillis":250,"memoryMiB":128}},"probes":{"liveness":{"path":"/healthz"},"readiness":{"path":"/readyz"}},"exposure":"Private"}}`, environment.PublicID)
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
	server.Config.ObservabilityLiveTTL = time.Millisecond
	server.Config.ObservabilityMetricsLivePoll = time.Hour
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
