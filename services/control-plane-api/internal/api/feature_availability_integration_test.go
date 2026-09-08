package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
)

func TestFeatureAvailabilityAPIEnforcesScopeAncestryAndReturnsSafeArrays(t *testing.T) {
	storage, workspaceID, ownerID, _ := newExecutorIntegrationFixture(t)
	workspace, err := storage.Workspace(t.Context(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(t.Context(), `UPDATE agent_installations SET last_seen_at=now() WHERE status='Active'`); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.PublicURL, config.AllowedOrigin, config.CookieName = "https://console.example", "https://console.example", "molejo_session"
	config.AllowedHosts = []string{"console.example"}
	server := NewServer(config, Dependencies{Store: storage, ProviderInventory: providerbinding.New(providerbinding.Binding{Capability: capabilitycontract.SourceGitHub, Configured: true, Health: providerbinding.HealthUnknown})})
	session := createAPISession(t, storage, ownerID, "availability-session", "availability-csrf")
	response := hierarchyRequest(t, server, session, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/feature-availability?scopeType=Workspace&scopeId="+workspace.PublicID, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var availability generated.FeatureAvailabilityResponse
	decodeResponse(t, response, &availability)
	if availability.Features == nil || len(availability.Features) == 0 {
		t.Fatal("features must be a non-null populated array")
	}
	if strings.Contains(response.Body.String(), "endpoint") || strings.Contains(response.Body.String(), "credential") {
		t.Fatalf("response exposed provider internals: %s", response.Body.String())
	}
	for _, item := range availability.Features {
		if item.Limitations == nil {
			t.Fatalf("%s limitations is null", item.Id)
		}
		if item.Id == string(capabilitycontract.SourceGitHub) && item.State != generated.FeatureAvailabilityStateUnknown {
			t.Fatalf("configured provider without health proof=%s", item.State)
		}
	}

	otherWorkspace := mustAPIID(t, "ws")
	otherProject, otherApp := mustAPIID(t, "prj"), mustAPIID(t, "app")
	var otherWorkspaceID, otherProjectID int64
	if err = storage.Pool.QueryRow(t.Context(), `INSERT INTO workspaces(public_id,name,namespace_name) VALUES($1,'Other','other-' || $2) RETURNING id`, otherWorkspace, time.Now().Format("150405.000000000")).Scan(&otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err = storage.Pool.QueryRow(t.Context(), `INSERT INTO projects(public_id,workspace_id,name,name_key) VALUES($1,$2,'Other','other') RETURNING id`, otherProject, otherWorkspaceID).Scan(&otherProjectID); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(t.Context(), `INSERT INTO apps(public_id,project_id,name,name_key) VALUES($1,$2,'Foreign','foreign')`, otherApp, otherProjectID); err != nil {
		t.Fatal(err)
	}
	response = hierarchyRequest(t, server, session, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/feature-availability?scopeType=App&scopeId="+otherApp, "", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign scope status=%d body=%s", response.Code, response.Body.String())
	}
}
