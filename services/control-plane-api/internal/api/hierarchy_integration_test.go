package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func TestHierarchyAPIEnforcesMembershipRoleAndDeploymentAncestry(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, ownerID, _ := newExecutorIntegrationFixture(t)
	workspace, err := storage.Workspace(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.PublicURL = "https://console.example"
	config.AllowedOrigin = "https://console.example"
	config.AllowedHosts = []string{"console.example"}
	config.CookieName = "molejo_session"
	server := NewServer(storage, nil, config, nil)
	owner := createAPISession(t, storage, ownerID, "owner-session", "owner-csrf")

	var workspaceAccepted struct {
		Workspace domain.Workspace `json:"workspace"`
		Operation domain.Operation `json:"operation"`
	}
	response := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces", `{"name":"Second Workspace"}`, map[string]string{"Idempotency-Key": "second-workspace"})
	if response.Code != http.StatusAccepted {
		t.Fatalf("create workspace status=%d body=%s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &workspaceAccepted)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = storage.Pool.Exec(cleanupCtx, `DELETE FROM operations WHERE workspace_id=(SELECT id FROM workspaces WHERE public_id=$1)`, workspaceAccepted.Workspace.PublicID)
		_, _ = storage.Pool.Exec(cleanupCtx, `DELETE FROM workspace_memberships WHERE workspace_id=(SELECT id FROM workspaces WHERE public_id=$1)`, workspaceAccepted.Workspace.PublicID)
		_, _ = storage.Pool.Exec(cleanupCtx, `DELETE FROM workspaces WHERE public_id=$1`, workspaceAccepted.Workspace.PublicID)
	})
	if workspaceAccepted.Workspace.BootstrapState == "Ready" {
		t.Fatal("new workspace was reported Ready before its runtime operation")
	}
	var retriedWorkspace struct {
		Workspace domain.Workspace `json:"workspace"`
		Operation domain.Operation `json:"operation"`
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces", `{"name":"Second Workspace"}`, map[string]string{"Idempotency-Key": "second-workspace"})
	decodeResponse(t, response, &retriedWorkspace)
	if response.Code != http.StatusAccepted || retriedWorkspace.Workspace.PublicID != workspaceAccepted.Workspace.PublicID || retriedWorkspace.Operation.PublicID != workspaceAccepted.Operation.PublicID {
		t.Fatalf("workspace retry status=%d first=%+v retry=%+v", response.Code, workspaceAccepted, retriedWorkspace)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspaceAccepted.Workspace.PublicID, `{"name":"Renamed Workspace"}`, map[string]string{"If-Match": "1"})
	if response.Code != http.StatusOK {
		t.Fatalf("update workspace status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspaceAccepted.Workspace.PublicID, `{"name":"Stale Workspace"}`, map[string]string{"If-Match": "1"})
	if response.Code != http.StatusConflict {
		t.Fatalf("stale workspace update status=%d body=%s", response.Code, response.Body.String())
	}
	var ownerWorkspaces struct {
		Items []domain.Workspace `json:"items"`
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces", "", nil)
	decodeResponse(t, response, &ownerWorkspaces)
	if response.Code != http.StatusOK || len(ownerWorkspaces.Items) != 2 {
		t.Fatalf("owner workspace list status=%d items=%d", response.Code, len(ownerWorkspaces.Items))
	}

	project := domain.Project{}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Customer Portal"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create project status=%d body=%s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &project)

	environment := domain.Environment{}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/environments", `{"name":"Production"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create environment status=%d body=%s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &environment)

	app := domain.App{}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/apps", `{"name":"Web"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create app status=%d body=%s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &app)
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID, `{"name":"Customer Platform"}`, map[string]string{"If-Match": "1"})
	if response.Code != http.StatusOK {
		t.Fatalf("update project status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID, `{"name":"Stale"}`, map[string]string{"If-Match": "1"})
	if response.Code != http.StatusConflict {
		t.Fatalf("stale project update status=%d body=%s", response.Code, response.Body.String())
	}

	otherProject := domain.Project{}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Other Project"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create other project status=%d body=%s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &otherProject)
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+otherProject.PublicID+"/apps/"+app.PublicID, `{"name":"Leaked"}`, map[string]string{"If-Match": "1"})
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), app.PublicID) {
		t.Fatalf("cross-project mutation status=%d body=%s", response.Code, response.Body.String())
	}

	configuration := fmt.Sprintf(`{"environmentId":%q,"branch":"main","workloadKind":"Stateless","configuration":{"replicas":1,"ports":[{"name":"http","containerPort":8080,"protocol":"TCP"}],"resources":{"requests":{"cpuMillis":50,"memoryMiB":64},"limits":{"cpuMillis":250,"memoryMiB":128}},"probes":{"startup":{"type":"HTTP","portName":"http","path":"/readyz"},"liveness":{"type":"HTTP","portName":"http","path":"/healthz"},"readiness":{"type":"HTTP","portName":"http","path":"/readyz"}},"publicEndpoints":[],"variables":[],"parameters":[]}}`, environment.PublicID)
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/apps/"+app.PublicID+"/environments", configuration, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create App Environment status=%d body=%s", response.Code, response.Body.String())
	}
	var target domain.AppEnvironment
	decodeResponse(t, response, &target)
	if target.AppPublicID != app.PublicID || target.EnvironmentPublicID != environment.PublicID || target.ProjectPublicID != project.PublicID {
		t.Fatalf("App Environment hierarchy=%+v", target)
	}
	var environmentApps struct {
		Items []struct {
			ID      string `json:"id"`
			AppID   string `json:"appId"`
			AppName string `json:"appName"`
		} `json:"items"`
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/environments/"+environment.PublicID+"/apps", "", nil)
	decodeResponse(t, response, &environmentApps)
	if response.Code != http.StatusOK || len(environmentApps.Items) != 1 || environmentApps.Items[0].ID != target.PublicID || environmentApps.Items[0].AppID != app.PublicID || environmentApps.Items[0].AppName != app.Name {
		t.Fatalf("environment Apps status=%d items=%+v", response.Code, environmentApps.Items)
	}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/apps/"+app.PublicID, "", map[string]string{"If-Match": "1"})
	if response.Code != http.StatusConflict {
		t.Fatalf("archive app with App Environment status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/environments/"+environment.PublicID, "", map[string]string{"If-Match": "1"})
	if response.Code != http.StatusConflict {
		t.Fatalf("archive environment with App Environment status=%d body=%s", response.Code, response.Body.String())
	}

	testerID := insertTester(t, storage, workspaceID)
	tester := createAPISession(t, storage, testerID, "tester-session", "tester-csrf")
	var testerWorkspaces struct {
		Items []domain.Workspace `json:"items"`
	}
	response = hierarchyRequest(t, server, tester, http.MethodGet, "/api/v1/workspaces", "", nil)
	decodeResponse(t, response, &testerWorkspaces)
	if response.Code != http.StatusOK || len(testerWorkspaces.Items) != 1 || testerWorkspaces.Items[0].PublicID != workspace.PublicID {
		t.Fatalf("tester workspace list status=%d items=%+v", response.Code, testerWorkspaces.Items)
	}
	response = hierarchyRequest(t, server, tester, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Forbidden"}`, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("tester mutation status=%d body=%s", response.Code, response.Body.String())
	}
	var testerPublicID string
	if err = storage.Pool.QueryRow(ctx, `SELECT public_id FROM users WHERE id=$1`, testerID).Scan(&testerPublicID); err != nil {
		t.Fatal(err)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/access-grants", fmt.Sprintf(`{"subjectType":"User","subjectId":%q,"resourceType":"Project","resourceId":%q,"relation":"Editor"}`, testerPublicID, project.PublicID), nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("grant project editor status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, tester, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID, `{"name":"Viewer Edited Project"}`, map[string]string{"If-Match": "2"})
	if response.Code != http.StatusOK {
		t.Fatalf("project editor mutation status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, tester, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+otherProject.PublicID, `{"name":"Forbidden Other Project"}`, map[string]string{"If-Match": "1"})
	if response.Code != http.StatusForbidden {
		t.Fatalf("project editor crossed resource boundary status=%d body=%s", response.Code, response.Body.String())
	}

	otherWorkspaceID, err := domain.NewPublicID("ws")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(ctx, `INSERT INTO workspaces(public_id,name,namespace_name) VALUES ($1,'Other',$1)`, otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = storage.Pool.Exec(context.Background(), `DELETE FROM workspaces WHERE public_id=$1`, otherWorkspaceID)
	})
	response = hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+otherWorkspaceID, "", nil)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Other") {
		t.Fatalf("cross-workspace response status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHierarchyAPIRejectsOutOfContractPagination(t *testing.T) {
	_, _, server, owner := newHierarchyAPITestFixture(t)

	for _, limit := range []string{"0", "-1", "101", "9223372036854775808"} {
		t.Run(limit, func(t *testing.T) {
			response := hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces?limit="+limit, "", nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("limit=%s returned status=%d body=%s", limit, response.Code, response.Body.String())
			}
		})
	}
}

func TestHierarchyAPIReportsConflictReasonsPrecisely(t *testing.T) {
	_, workspace, server, owner := newHierarchyAPITestFixture(t)

	response := hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID, `{"name":"Stale"}`, map[string]string{"If-Match": fmt.Sprint(workspace.Version + 1)})
	assertHierarchyConflict(t, response, "version_conflict", "resource changed since it was read")

	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Portal"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create project returned status=%d body=%s", response.Code, response.Body.String())
	}
	var project domain.Project
	decodeResponse(t, response, &project)
	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":" PORTAL "}`, nil)
	assertHierarchyConflict(t, response, "name_conflict", "an active resource already uses this name")

	response = hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID+"/environments", `{"name":"Production"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create environment returned status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, "/api/v1/workspaces/"+workspace.PublicID+"/projects/"+project.PublicID, "", map[string]string{"If-Match": fmt.Sprint(project.Version)})
	assertHierarchyConflict(t, response, "dependency_conflict", "resource has active dependencies")
}

func TestAuthorizationResourceUsesTheMostSpecificSupportedResource(t *testing.T) {
	workspaceID := "ws-aaaaaaaaaaaaaaaaaaaa"
	tests := []struct{ path, resourceType, resourceID string }{
		{"/api/v1/workspaces/" + workspaceID + "/settings", "Workspace", workspaceID},
		{"/api/v1/workspaces/" + workspaceID + "/projects/prj-aaaaaaaaaaaaaaaaaaaa/environments/env-bbbbbbbbbbbbbbbbbbbb", "Project", "prj-aaaaaaaaaaaaaaaaaaaa"},
		{"/api/v1/workspaces/" + workspaceID + "/projects/prj-aaaaaaaaaaaaaaaaaaaa/apps/app-bbbbbbbbbbbbbbbbbbbb/source", "App", "app-bbbbbbbbbbbbbbbbbbbb"},
		{"/api/v1/workspaces/" + workspaceID + "/projects/prj-aaaaaaaaaaaaaaaaaaaa/apps/app-bbbbbbbbbbbbbbbbbbbb/environments/aev-cccccccccccccccccccc/deployments", "AppEnvironment", "aev-cccccccccccccccccccc"},
	}
	for _, test := range tests {
		resourceType, resourceID := authorizationResource(test.path, workspaceID)
		if resourceType != test.resourceType || resourceID != test.resourceID {
			t.Fatalf("path=%s resource=%s/%s, want %s/%s", test.path, resourceType, resourceID, test.resourceType, test.resourceID)
		}
	}
}

func assertHierarchyConflict(t *testing.T, response *httptest.ResponseRecorder, code, message string) {
	t.Helper()
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict returned status=%d body=%s", response.Code, response.Body.String())
	}
	var apiError struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	decodeResponse(t, response, &apiError)
	if apiError.Code != code || apiError.Message != message {
		t.Fatalf("conflict code=%q message=%q, want code=%q message=%q", apiError.Code, apiError.Message, code, message)
	}
}

func newHierarchyAPITestFixture(t *testing.T) (*store.Store, domain.Workspace, *Server, apiSession) {
	t.Helper()
	storage, workspaceID, ownerID, _ := newExecutorIntegrationFixture(t)
	workspace, err := storage.Workspace(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.PublicURL = "https://console.example"
	config.AllowedOrigin = "https://console.example"
	config.AllowedHosts = []string{"console.example"}
	config.CookieName = "molejo_session"
	server := NewServer(storage, nil, config, nil)
	owner := createAPISession(t, storage, ownerID, "focused-owner-session", "focused-owner-csrf")
	return storage, workspace, server, owner
}

type apiSession struct {
	Token string
	CSRF  string
}

func createAPISession(t *testing.T, storage interface {
	CreateSession(context.Context, int64, []byte, []byte, time.Time) error
}, actorID int64, token, csrf string,
) apiSession {
	t.Helper()
	if err := storage.CreateSession(context.Background(), actorID, auth.HashToken(token), auth.HashToken(csrf), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return apiSession{Token: token, CSRF: csrf}
}

func hierarchyRequest(t *testing.T, server *Server, session apiSession, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Host = "console.example"
	request.Header.Set("Origin", "https://console.example")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRF)
	request.AddCookie(&http.Cookie{Name: "molejo_session", Value: session.Token})
	request.AddCookie(&http.Cookie{Name: "molejo_session_csrf", Value: session.CSRF})
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func insertTester(t *testing.T, storage *store.Store, workspaceID int64) int64 {
	t.Helper()
	actorKey := fmt.Sprintf("hierarchy-tester-%d", time.Now().UnixNano())
	var actorID int64
	if err := storage.Pool.QueryRow(context.Background(), `INSERT INTO users(public_id,username,username_key,display_name,status) VALUES ($1,$2,$2,$2,'Active') RETURNING id`, mustAPIID(t, "usr"), actorKey).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Pool.Exec(context.Background(), `INSERT INTO password_credentials(user_id,password_hash) VALUES ($1,'integration-only')`, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Pool.Exec(context.Background(), `INSERT INTO workspace_memberships(workspace_id,user_id,role,status) VALUES ($1,$2,'Viewer','Active')`, workspaceID, actorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = storage.Pool.Exec(context.Background(), `DELETE FROM sessions WHERE user_id=$1`, actorID)
		_, _ = storage.Pool.Exec(context.Background(), `DELETE FROM workspace_memberships WHERE user_id=$1`, actorID)
		_, _ = storage.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, actorID)
	})
	return actorID
}
