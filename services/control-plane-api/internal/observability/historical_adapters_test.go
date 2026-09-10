package observability

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	_ HistoricalLogReader    = (*ClickHouseClient)(nil)
	_ HistoricalEventReader  = (*ClickHouseClient)(nil)
	_ HistoricalMetricReader = (*PrometheusQueryAdapter)(nil)
)

func TestClickHouseLogsAlwaysScopeQueriesToRuntime(t *testing.T) {
	t.Parallel()

	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form = r.Form
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"id":"11111111-1111-4111-8111-111111111111","timestamp_ns":1787918400123456789,"ingested_at_ns":1787918401123456789,"body":"ready","severity":"INFO","instance":"pod-1","container":"app"}` + "\n"))
	}))
	defer server.Close()

	client, err := NewClickHouseClient(server.URL, "otel", "reader", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Logs(context.Background(), Scope{Namespace: "workspace-a", RuntimeName: "runtime-a"}, LogQuery{
		From: time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 28, 12, 1, 0, 0, time.UTC), Search: "ready", Limit: 50,
		Snapshot: LogCursor{IngestedAt: time.Date(2026, 8, 28, 12, 2, 0, 0, time.UTC), ID: maxLogUUID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Body != "ready" || page.Items[0].ID != "log-11111111111141118111111111111111" {
		t.Fatalf("unexpected items: %#v", page.Items)
	}
	if got := page.Items[0].Timestamp.Nanosecond(); got != 123456789 {
		t.Fatalf("timestamp nanoseconds = %d, want 123456789", got)
	}
	for key, want := range map[string]string{"param_namespace": "workspace-a", "param_runtime": "runtime-a", "param_search": "ready", "param_limit": "51"} {
		if got := form.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if got := form.Get("param_from"); got != "2026-08-28 11:00:00.000000000" {
		t.Fatalf("param_from = %q, want ClickHouse DateTime64 format", got)
	}
	query := form.Get("query")
	if !strings.Contains(query, "MolejoNamespace") || !strings.Contains(query, "MolejoRuntime") || !strings.Contains(query, "MolejoLogId") || !strings.Contains(query, "MolejoIngestedAt") || !strings.Contains(query, "toUnixTimestamp64Nano") {
		t.Fatalf("query is not tenant and runtime scoped: %s", query)
	}
}

func TestPrometheusQueryAdapterAlwaysScopesEveryMetricQuery(t *testing.T) {
	t.Parallel()

	var queries []string
	var queriesMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queriesMu.Lock()
		queries = append(queries, r.URL.Query().Get("query"))
		queriesMu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}))
	defer server.Close()

	client, err := NewPrometheusQueryAdapter(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{ClusterID: "cls-a", ClusterUID: "uid-a", WorkspaceID: "ws-a", AppEnvironmentID: "aev-a", Namespace: "workspace-a", RuntimeName: "runtime-a"}
	response, err := client.Metrics(context.Background(), scope, MetricQuery{
		From: time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), Step: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Series) != len(metricDefinitions) || len(queries) != len(metricDefinitions) {
		t.Fatalf("got %d series and %d queries", len(response.Series), len(queries))
	}
	for _, query := range queries {
		assertRuntimeScopedMetricQuery(t, query, scope)
		if strings.Contains(query, "k8s_pod_name") {
			t.Fatalf("metric query exposes Kubernetes pod identity: %s", query)
		}
	}
}

func assertRuntimeScopedMetricQuery(t *testing.T, query string, scope Scope) {
	t.Helper()
	for label, value := range map[string]string{
		"molejo_cluster_id": scope.ClusterID, "molejo_cluster_uid": scope.ClusterUID, "molejo_workspace_id": scope.WorkspaceID,
		"molejo_app_environment_id": scope.AppEnvironmentID, "k8s_namespace_name": scope.Namespace, "molejo_app_environment_runtime": scope.RuntimeName,
	} {
		if !strings.Contains(query, label+`="`+value+`"`) {
			t.Fatalf("metric query lacks %s identity: %s", label, query)
		}
	}
	if strings.Contains(query, "k8s_deployment_available") {
		if !strings.Contains(query, `k8s_deployment_name="runtime-a"`) || !strings.Contains(query, `k8s_statefulset_name="runtime-a"`) || !strings.Contains(query, "k8s_statefulset_ready_pods") || !strings.Contains(query, " or ") {
			t.Fatalf("available replicas query does not cover Deployment and StatefulSet: %s", query)
		}
		return
	}
	if strings.Contains(query, "k8s_deployment_desired") {
		if !strings.Contains(query, `k8s_deployment_name="runtime-a"`) || !strings.Contains(query, `k8s_statefulset_name="runtime-a"`) || !strings.Contains(query, "k8s_statefulset_desired_pods") || !strings.Contains(query, " or ") {
			t.Fatalf("desired replicas query does not cover Deployment and StatefulSet: %s", query)
		}
		return
	}
	if !strings.Contains(query, `molejo_app_environment_runtime="runtime-a"`) {
		t.Fatalf("pod metric query is not scoped by the product runtime label: %s", query)
	}
	if strings.Contains(query, "k8s_deployment_name") || strings.Contains(query, "k8s_statefulset_name") {
		t.Fatalf("pod metric query is coupled to a Kubernetes workload kind: %s", query)
	}
}

func TestPrometheusQueryAdapterConformanceRequiresMolejoIdentitySchema(t *testing.T) {
	t.Parallel()
	scope := Scope{ClusterID: "cls-a", ClusterUID: "uid-a", WorkspaceID: "ws-a", AppEnvironmentID: "aev-a", Namespace: "workspace-a", RuntimeName: "runtime-a"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		query := r.URL.Query().Get("query")
		for _, label := range []string{"molejo_cluster_id", "molejo_cluster_uid", "molejo_workspace_id", "molejo_app_environment_id", "k8s_namespace_name", "molejo_app_environment_runtime"} {
			if !strings.Contains(query, label) {
				t.Fatalf("conformance query lacks %s: %s", label, query)
			}
		}
		if !strings.Contains(query, `molejo_cluster_id="cls-a"`) || !strings.Contains(query, `molejo_cluster_uid="uid-a"`) {
			t.Fatalf("conformance query lacks cluster identity: %s", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{map[string]any{"metric": map[string]string{
			"molejo_cluster_id": scope.ClusterID, "molejo_cluster_uid": scope.ClusterUID, "molejo_workspace_id": scope.WorkspaceID,
			"molejo_app_environment_id": scope.AppEnvironmentID, "k8s_namespace_name": scope.Namespace, "molejo_app_environment_runtime": scope.RuntimeName,
		}, "value": []any{time.Now().Unix(), "1"}}}}})
	}))
	defer server.Close()
	client, err := NewPrometheusQueryAdapter(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := client.CheckConformance(context.Background(), scope)
	if err != nil || !evidence.Conformant || evidence.ReasonCode != "" {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

func TestPrometheusQueryAdapterConformanceKeepsEmptySchemaUnproven(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{}}})
	}))
	defer server.Close()
	client, _ := NewPrometheusQueryAdapter(server.URL, server.Client())
	evidence, err := client.CheckConformance(context.Background(), Scope{ClusterID: "cls-a", ClusterUID: "uid-a"})
	if err != nil || evidence.Conformant || evidence.ReasonCode != "metrics_schema_unproven" {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

func TestPrometheusQueryAdapterReturnsExplicitPartialResults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("query"), "memory") {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}))
	defer server.Close()
	client, err := NewPrometheusQueryAdapter(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := client.Metrics(context.Background(), Scope{Namespace: "workspace-a", RuntimeName: "runtime-a"}, MetricQuery{From: time.Now().Add(-time.Hour), To: time.Now(), Step: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if !metrics.Partial || len(metrics.Unavailable) != 1 || metrics.Unavailable[0] != "memory" || len(metrics.Series) != len(metricDefinitions) {
		t.Fatalf("unexpected partial response: %#v", metrics)
	}
}

func TestClickHouseEventsUseKubernetesEventAttributesAndRuntimePrefix(t *testing.T) {
	t.Parallel()

	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form = r.Form
		_, _ = w.Write([]byte(`{"timestamp":"2026-08-28T12:00:00Z","type":"Normal","reason":"Started"}` + "\n"))
	}))
	defer server.Close()
	client, err := NewClickHouseClient(server.URL, "otel", "reader", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.Events(context.Background(), Scope{Namespace: "workspace-a", RuntimeName: "runtime-a"}, EventQuery{From: time.Now().Add(-time.Hour), To: time.Now(), Limit: 25})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	query := form.Get("query")
	if !strings.Contains(query, "LogAttributes['k8s.namespace.name']") || !strings.Contains(query, "startsWith(LogAttributes['k8s.event.name'], {runtime:String})") {
		t.Fatalf("event query does not use collector event attributes: %s", query)
	}
	if items[0].Source != "kubernetes" || items[0].Message != "Runtime container started." {
		t.Fatalf("event exposes an unstable Kubernetes detail: %#v", items[0])
	}
}

func TestRuntimeEventMessageNeverReturnsRawInfrastructureText(t *testing.T) {
	t.Parallel()

	if got := runtimeEventMessage("UnknownProviderReason"); got != "Runtime event recorded." {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeTextBoundsAndRemovesControlCharacters(t *testing.T) {
	t.Parallel()

	got := sanitizeText("ok\x00\x01\n" + strings.Repeat("x", maxTextBytes+10))
	if strings.ContainsAny(got, "\x00\x01") {
		t.Fatalf("control characters were preserved: %q", got)
	}
	if len(got) > maxTextBytes {
		t.Fatalf("text has %d bytes", len(got))
	}
}

func TestUnavailableHistoricalReaderHasStableError(t *testing.T) {
	t.Parallel()

	_, err := (UnavailableHistoricalReader{}).Logs(context.Background(), Scope{}, LogQuery{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}
