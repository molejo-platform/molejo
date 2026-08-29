package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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
		_, _ = w.Write([]byte(`{"id":"11111111-1111-4111-8111-111111111111","timestamp":"2026-08-28T12:00:00.123456789Z","ingested_at":"2026-08-28T12:00:01.123456789Z","body":"ready","severity":"INFO","instance":"pod-1","container":"app"}` + "\n"))
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
	for key, want := range map[string]string{"param_namespace": "workspace-a", "param_runtime": "runtime-a", "param_search": "ready", "param_limit": "51"} {
		if got := form.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if got := form.Get("param_from"); got != "2026-08-28 11:00:00.000000000" {
		t.Fatalf("param_from = %q, want ClickHouse DateTime64 format", got)
	}
	query := form.Get("query")
	if !strings.Contains(query, "k8s.namespace.name") || !strings.Contains(query, "k8s.deployment.name") || !strings.Contains(query, "MolejoLogId") || !strings.Contains(query, "MolejoIngestedAt") {
		t.Fatalf("query is not tenant and runtime scoped: %s", query)
	}
}

func TestVictoriaMetricsAlwaysScopesEveryMetricQuery(t *testing.T) {
	t.Parallel()

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("query"))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}))
	defer server.Close()

	client, err := NewVictoriaMetricsClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Metrics(context.Background(), Scope{Namespace: "workspace-a", RuntimeName: "runtime-a"}, MetricQuery{
		From: time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), Step: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Series) != len(metricDefinitions) || len(queries) != len(metricDefinitions) {
		t.Fatalf("got %d series and %d queries", len(response.Series), len(queries))
	}
	for _, query := range queries {
		if !strings.Contains(query, `k8s_namespace_name="workspace-a"`) || !strings.Contains(query, `k8s_deployment_name="runtime-a"`) {
			t.Fatalf("metric query is not tenant and runtime scoped: %s", query)
		}
	}
}

func TestVictoriaMetricsCurrentMetricsUsesInstantScopedQueries(t *testing.T) {
	t.Parallel()

	var paths, queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		queries = append(queries, r.URL.Query().Get("query"))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{
			map[string]any{"metric": map[string]string{"k8s_pod_name": "pod-a"}, "value": []any{1_787_918_400, "1.5"}},
		}}})
	}))
	defer server.Close()

	client, err := NewVictoriaMetricsClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.CurrentMetrics(context.Background(), Scope{Namespace: "workspace-a", RuntimeName: "runtime-a"}, time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Samples) != len(metricDefinitions) || len(paths) != len(metricDefinitions) {
		t.Fatalf("got %d samples and %d queries", len(snapshot.Samples), len(paths))
	}
	for index, path := range paths {
		if path != "/api/v1/query" {
			t.Fatalf("path[%d] = %q", index, path)
		}
		if !strings.Contains(queries[index], `k8s_namespace_name="workspace-a"`) || !strings.Contains(queries[index], `k8s_deployment_name="runtime-a"`) {
			t.Fatalf("metric query is not tenant and runtime scoped: %s", queries[index])
		}
	}
	if snapshot.Samples[0].Name != "cpu" || snapshot.Samples[0].Instance != "pod-a" || snapshot.Samples[0].Value != 1.5 {
		t.Fatalf("unexpected sample: %#v", snapshot.Samples[0])
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
	if items[0].Source != "kubernetes" || items[0].Message != "Runtime container started." || items[0].Instance != "" {
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

func TestUnavailableReaderHasStableError(t *testing.T) {
	t.Parallel()

	_, err := (UnavailableReader{}).Logs(context.Background(), Scope{}, LogQuery{})
	if err != ErrUnavailable {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}
