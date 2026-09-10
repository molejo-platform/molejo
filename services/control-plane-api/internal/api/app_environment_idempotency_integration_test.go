package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestDeleteAppEnvironmentReplaysAfterArchival(t *testing.T) {
	storage, workspace, server, owner := newHierarchyAPITestFixture(t)

	response := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/projects", `{"name":"Delete Replay"}`, nil)
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
	response = hierarchyRequest(t, server, owner, http.MethodPost, setupPath, applicationSetupPayload(environment.PublicID, testAgentInstallationID, "Replay App"), map[string]string{"Idempotency-Key": "delete-replay-setup"})
	if response.Code != http.StatusCreated {
		t.Fatalf("create app environment status=%d body=%s", response.Code, response.Body.String())
	}
	var setup struct {
		App            domain.App            `json:"app"`
		AppEnvironment domain.AppEnvironment `json:"appEnvironment"`
	}
	decodeResponse(t, response, &setup)

	deletePath := "/api/v1/workspaces/" + workspace.PublicID + "/projects/" + project.PublicID + "/apps/" + setup.App.PublicID + "/environments/" + setup.AppEnvironment.PublicID
	headers := map[string]string{"If-Match": "1", "Idempotency-Key": "delete-replay"}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, deletePath, "", headers)
	if response.Code != http.StatusAccepted {
		t.Fatalf("first delete status=%d body=%s", response.Code, response.Body.String())
	}
	var first domain.Operation
	decodeResponse(t, response, &first)

	if _, err := storage.Pool.Exec(context.Background(), `UPDATE app_environments SET archived_at=now() WHERE public_id=$1`, setup.AppEnvironment.PublicID); err != nil {
		t.Fatal(err)
	}

	response = hierarchyRequest(t, server, owner, http.MethodDelete, deletePath, "", headers)
	if response.Code != http.StatusAccepted {
		t.Fatalf("replayed delete status=%d body=%s", response.Code, response.Body.String())
	}
	var replayed domain.Operation
	decodeResponse(t, response, &replayed)
	if replayed.PublicID != first.PublicID {
		t.Fatalf("replayed operation=%q, want %q", replayed.PublicID, first.PublicID)
	}
}
