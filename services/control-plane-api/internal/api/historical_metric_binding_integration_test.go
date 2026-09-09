package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
)

func TestHistoricalMetricBindingRequiresAdministratorAndDrivesAvailability(t *testing.T) {
	storage, workspace, server, owner := newHierarchyAPITestFixture(t)
	if _, err := storage.Pool.Exec(t.Context(), `UPDATE agent_installations SET last_seen_at=now() WHERE public_id=$1`, testAgentInstallationID); err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Fatalf("probe path=%s", r.URL.Path)
		}
		query := r.URL.Query().Get("query")
		if !strings.Contains(query, `molejo_cluster_id="`+testAgentInstallationID+`"`) || !strings.Contains(query, `molejo_cluster_uid="cluster-test-uid"`) {
			t.Fatalf("unscoped probe=%s", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{map[string]any{"metric": map[string]string{
			"molejo_cluster_id": testAgentInstallationID, "molejo_cluster_uid": "cluster-test-uid", "molejo_workspace_id": workspace.PublicID,
			"molejo_app_environment_id": "aev-aaaaaaaaaaaaaaaaaaaa", "k8s_namespace_name": workspace.Namespace, "molejo_app_environment_runtime": "runtime-a",
		}, "value": []any{time.Now().Unix(), "1"}}}}})
	}))
	defer provider.Close()

	path := "/api/v1/admin/clusters/" + testAgentInstallationID + "/bindings/historical-metrics"
	availabilityPath := "/api/v1/workspaces/" + workspace.PublicID + "/feature-availability?scopeType=Workspace&scopeId=" + workspace.PublicID
	assertHistoricalMetricAvailability(t, server, owner, availabilityPath, generated.FeatureAvailabilityStateNotConfigured)

	testerID := insertTester(t, storage, workspace.ID)
	tester := createAPISession(t, storage, testerID, "binding-tester-session", "binding-tester-csrf")
	response := hierarchyRequest(t, server, tester, http.MethodPut, path, `{"provider":"PrometheusCompatible","endpoint":"`+provider.URL+`"}`, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d body=%s", response.Code, response.Body.String())
	}

	response = hierarchyRequest(t, server, owner, http.MethodPut, path, `{"provider":"PrometheusCompatible","endpoint":"`+provider.URL+`"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), provider.URL) || strings.Contains(response.Body.String(), "endpoint") {
		t.Fatalf("response exposed private provider configuration: %s", response.Body.String())
	}
	var binding generated.HistoricalMetricBinding
	decodeResponse(t, response, &binding)
	if !binding.Conformant || binding.Health != generated.HistoricalMetricBindingHealthHealthy || binding.Version != 1 {
		t.Fatalf("binding=%+v", binding)
	}
	var auditCount int
	if err := storage.Pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_events WHERE action='installation.binding.historical_metrics.put' AND target_public_id=$1`, testAgentInstallationID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
	assertHistoricalMetricAvailability(t, server, owner, availabilityPath, generated.FeatureAvailabilityStateAvailable)

	response = hierarchyRequest(t, server, owner, http.MethodPut, path, `{"provider":"PrometheusCompatible","endpoint":"`+provider.URL+`"}`, map[string]string{"If-Match": "9"})
	if response.Code != http.StatusConflict {
		t.Fatalf("stale update status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, path, "", map[string]string{"If-Match": "1"})
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	assertHistoricalMetricAvailability(t, server, owner, availabilityPath, generated.FeatureAvailabilityStateNotConfigured)
}

func assertHistoricalMetricAvailability(t *testing.T, server *Server, session apiSession, path string, want generated.FeatureAvailabilityState) {
	t.Helper()
	response := hierarchyRequest(t, server, session, http.MethodGet, path, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("availability status=%d body=%s", response.Code, response.Body.String())
	}
	var availability generated.FeatureAvailabilityResponse
	decodeResponse(t, response, &availability)
	for _, feature := range availability.Features {
		if feature.Id == string(capabilitycontract.TelemetryMetricsHistorical) {
			if feature.State != want {
				t.Fatalf("historical metrics=%s want=%s body=%s", feature.State, want, response.Body.String())
			}
			return
		}
	}
	t.Fatal("historical metric feature missing")
}
