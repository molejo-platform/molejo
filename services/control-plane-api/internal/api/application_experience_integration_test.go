package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestApplicationExperienceCreatesAtomicallyAndExposesWorkspaceFacts(t *testing.T) {
	storage, workspace, server, owner := newHierarchyAPITestFixture(t)

	response := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Customer Portal"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create project status=%d body=%s", response.Code, response.Body.String())
	}
	var project domain.Project
	decodeResponse(t, response, &project)
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/environments", `{"name":"Production"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create environment status=%d body=%s", response.Code, response.Body.String())
	}
	var environment domain.Environment
	decodeResponse(t, response, &environment)

	setupPath := "/api/v1/workspaces/" + workspace.PublicID + "/projects/" + project.PublicID + "/app-environments"
	payload := applicationSetupPayload(environment.PublicID, testAgentInstallationID, "Console API")
	response = hierarchyRequest(t, server, owner, http.MethodPost, setupPath, payload, map[string]string{"Idempotency-Key": "console-api-setup"})
	if response.Code != http.StatusCreated {
		t.Fatalf("create setup status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		App            domain.App            `json:"app"`
		AppEnvironment domain.AppEnvironment `json:"appEnvironment"`
	}
	decodeResponse(t, response, &created)
	if created.App.Name != "Console API" || created.AppEnvironment.AppPublicID != created.App.PublicID || created.AppEnvironment.SourceBranch != "" {
		t.Fatalf("created setup=%+v", created)
	}

	response = hierarchyRequest(t, server, owner, http.MethodPost, setupPath, payload, map[string]string{"Idempotency-Key": "console-api-setup"})
	var retried struct {
		App            domain.App            `json:"app"`
		AppEnvironment domain.AppEnvironment `json:"appEnvironment"`
	}
	decodeResponse(t, response, &retried)
	if response.Code != http.StatusCreated || retried.App.PublicID != created.App.PublicID || retried.AppEnvironment.PublicID != created.AppEnvironment.PublicID {
		t.Fatalf("idempotent retry status=%d setup=%+v", response.Code, retried)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, setupPath, applicationSetupPayload(environment.PublicID, testAgentInstallationID, "Changed"), map[string]string{"Idempotency-Key": "console-api-setup"})
	assertHierarchyConflict(t, response, "idempotency_conflict", "idempotency key was already used with a different request")

	response = hierarchyRequest(t, server, owner, http.MethodPost, setupPath, applicationSetupPayload(environment.PublicID, "cls-aaaaaaaaaaaaaaaaaaaa", "Rolled Back"), map[string]string{"Idempotency-Key": "rollback-setup"})
	assertHierarchyConflict(t, response, "cluster_unavailable", "the selected cluster is unavailable or the workspace is not ready on it")
	var rolledBackCount int
	if err := storage.Pool.QueryRow(context.Background(), `SELECT count(*) FROM apps WHERE name_key='rolled back'`).Scan(&rolledBackCount); err != nil {
		t.Fatal(err)
	}
	if rolledBackCount != 0 {
		t.Fatalf("failed setup left %d Apps behind", rolledBackCount)
	}

	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/summary", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"appEnvironments":1`) {
		t.Fatalf("workspace summary status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/operations?limit=50", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items"`) {
		t.Fatalf("workspace operations status=%d body=%s", response.Code, response.Body.String())
	}
}

func applicationSetupPayload(environmentID, clusterID, name string) string {
	return fmt.Sprintf(`{"app":{"mode":"New","name":%q},"environmentId":%q,"clusterId":%q,"workloadKind":"Stateless","configuration":{"replicas":1,"ports":[{"name":"http","containerPort":8080,"protocol":"TCP"}],"resources":{"requests":{"cpuMillis":50,"memoryMiB":64},"limits":{"cpuMillis":250,"memoryMiB":128}},"probes":{"startup":{"type":"HTTP","portName":"http","path":"/readyz"},"liveness":{"type":"HTTP","portName":"http","path":"/healthz"},"readiness":{"type":"HTTP","portName":"http","path":"/readyz"}},"publicEndpoints":[],"variables":[],"parameters":[]}}`, name, environmentID, clusterID)
}
