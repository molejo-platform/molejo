package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestAutomationAPIRegistersAndDeploysExternalRelease(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, ownerID, _ := newExecutorIntegrationFixture(t)
	workspace, err := storage.Workspace(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := storage.CreateProject(ctx, workspaceID, mustAPIID(t, "prj"), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := storage.CreateEnvironment(ctx, workspaceID, project.PublicID, mustAPIID(t, "env"), "Production", "production")
	if err != nil {
		t.Fatal(err)
	}
	app, err := storage.CreateApp(ctx, workspaceID, project.PublicID, mustAPIID(t, "app"), "API", "api")
	if err != nil {
		t.Fatal(err)
	}
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, ownerID, mustAPIID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", apiRuntimeConfiguration("automation-api"))
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.PublicURL = "https://console.example"
	config.AllowedOrigin = "https://console.example"
	config.AllowedHosts = []string{"console.example"}
	config.AllowedRegistries = []string{"registry.example"}
	server := NewServer(config, Dependencies{Store: storage})
	owner := createAPISession(t, storage, ownerID, "automation-owner-session", "automation-owner-csrf")
	base := "/api/v1/workspaces/" + workspace.PublicID + "/projects/" + project.PublicID + "/apps/" + app.PublicID

	response := hierarchyRequest(t, server, owner, http.MethodPost, base+"/service-accounts", `{"name":"Missing scope"}`, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing environment scope status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/service-accounts", `{"name":"GitHub Actions","deploymentEnvironmentIds":["`+target.PublicID+`"]}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create service account status=%d body=%s", response.Code, response.Body.String())
	}
	var account automation.ServiceAccount
	decodeResponse(t, response, &account)
	if account.Name != "GitHub Actions" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("account=%+v cache-control=%q", account, response.Header().Get("Cache-Control"))
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/service-accounts", `{"name":"GitHub Actions","deploymentEnvironmentIds":["`+target.PublicID+`"]}`, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"service_account_name_conflict"`) {
		t.Fatalf("duplicate service account status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/service-accounts/"+account.PublicID+"/tokens", `{}`, nil)
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create token status=%d cache-control=%q body=%s", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
	var credential automation.Credential
	decodeResponse(t, response, &credential)
	if credential.Token == "" || credential.TokenID == "" {
		t.Fatalf("credential=%+v", credential)
	}
	response = hierarchyRequest(t, server, apiSession{}, http.MethodGet, base+"/environments/"+target.PublicID, "", map[string]string{"Authorization": "Bearer " + credential.Token})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":1`) {
		t.Fatalf("read environment status=%d body=%s", response.Code, response.Body.String())
	}

	image := "registry.example/molejo/api@sha256:" + strings.Repeat("a", 64)
	releaseBody := `{"artifact":{"kind":"OCIImage","reference":"` + image + `"},"source":{"provider":"GitHub","repository":"molejo-platform/api","revision":"` + strings.Repeat("b", 40) + `","ref":"refs/heads/main"},"provenance":{"producer":"github-actions","externalRunId":"123","url":"https://github.com/molejo-platform/api/actions/runs/123"}}`
	automationHeaders := map[string]string{"Authorization": "Bearer " + credential.Token, "Idempotency-Key": "release-123"}
	response = hierarchyRequest(t, server, apiSession{}, http.MethodPost, base+"/releases", releaseBody, automationHeaders)
	if response.Code != http.StatusCreated {
		t.Fatalf("register release status=%d body=%s", response.Code, response.Body.String())
	}
	var registered domain.Release
	decodeResponse(t, response, &registered)
	if registered.Image != image || registered.OriginKind != "External" || registered.ProvenanceStatus != "Declared" || registered.CreatedBy.ID != account.PublicID || registered.CreatedBy.DisplayName != "GitHub Actions" {
		t.Fatalf("registered=%+v", registered)
	}
	response = hierarchyRequest(t, server, apiSession{}, http.MethodPost, base+"/releases", releaseBody, automationHeaders)
	if response.Code != http.StatusOK {
		t.Fatalf("replay release status=%d body=%s", response.Code, response.Body.String())
	}

	deploymentHeaders := map[string]string{"Authorization": "Bearer " + credential.Token, "Idempotency-Key": "deployment-123", "If-Match": "1"}
	response = hierarchyRequest(t, server, apiSession{}, http.MethodPost, base+"/environments/"+target.PublicID+"/deployments", `{"releaseId":"`+registered.PublicID+`","configurationVersion":1,"currentDeploymentId":null}`, deploymentHeaders)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create deployment status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"requestedBy":{"id":"`+account.PublicID+`","kind":"ServiceAccount","displayName":"GitHub Actions"}`) {
		t.Fatalf("deployment attribution body=%s", response.Body.String())
	}
	var accepted struct {
		Operation domain.Operation `json:"operation"`
	}
	decodeResponse(t, response, &accepted)
	response = hierarchyRequest(t, server, apiSession{}, http.MethodGet, "/api/v1/operations/"+accepted.Operation.PublicID, "", map[string]string{"Authorization": "Bearer " + credential.Token})
	if response.Code != http.StatusOK {
		t.Fatalf("read operation status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, base+"/service-accounts/"+account.PublicID+"/tokens", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), credential.TokenID) || strings.Contains(response.Body.String(), credential.Token) {
		t.Fatalf("list tokens status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/service-accounts/"+account.PublicID+"/tokens", `{}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("rotate token status=%d body=%s", response.Code, response.Body.String())
	}
	var replacement automation.Credential
	decodeResponse(t, response, &replacement)
	response = hierarchyRequest(t, server, owner, http.MethodDelete, base+"/service-accounts/"+account.PublicID+"/tokens/"+credential.TokenID, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("revoke token status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, apiSession{}, http.MethodGet, base+"/environments/"+target.PublicID, "", map[string]string{"Authorization": "Bearer " + credential.Token})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d body=%s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, apiSession{}, http.MethodGet, base+"/environments/"+target.PublicID, "", map[string]string{"Authorization": "Bearer " + replacement.Token})
	if response.Code != http.StatusOK {
		t.Fatalf("replacement token status=%d body=%s", response.Code, response.Body.String())
	}
}
