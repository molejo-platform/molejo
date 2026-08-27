package api

import (
	"errors"
	"net/http"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api/generated"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListAppBuilds(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, params generated.ListAppBuildsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.Store.ListBuilds(r.Context(), workspace.ID, string(projectID), string(appID), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list builds", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateAppBuild(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, _ generated.CreateAppBuildParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	idempotencyHash := auth.HashToken(idempotencyKey)
	payloadHash = scopedBuildPayloadHash(r, payloadHash)
	if existing, found, err := h.server.Store.FindBuildByIdempotency(r.Context(), workspace.ID, actor.ID, idempotencyHash, payloadHash); err != nil {
		writeBuildError(w, r, err)
		return
	} else if found {
		writeJSON(w, http.StatusAccepted, existing)
		return
	}
	if !h.githubAvailable(w, r) {
		return
	}
	source, err := h.server.Store.GitHubBuildSource(r.Context(), workspace.ID, string(projectID), string(appID))
	if err != nil {
		writeBuildError(w, r, err)
		return
	}
	commitSHA, err := h.server.GitHub.ResolveCommit(r.Context(), source.InstallationExternalID, source.RepositoryID, source.DefaultBranch)
	if err != nil {
		h.server.logger().Warn("resolve GitHub build commit", "request_id", requestID(r), "app_id", source.AppPublicID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_unavailable", "GitHub commit could not be resolved", r)
		return
	}
	for range 3 {
		publicID, idErr := domain.NewPublicID("bld")
		if idErr != nil {
			break
		}
		build, _, createErr := h.server.Store.CreateBuild(r.Context(), workspace.ID, actor.ID, publicID, string(projectID), string(appID), commitSHA, idempotencyHash, payloadHash)
		if errors.Is(createErr, store.ErrPublicIDCollision) {
			continue
		}
		if createErr != nil {
			writeBuildError(w, r, createErr)
			return
		}
		writeJSON(w, http.StatusAccepted, build)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a build identifier", r)
}

func (h *generatedHandler) GetAppBuild(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, buildID generated.BuildId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	build, ok := h.appBuild(w, r, workspace.ID, string(projectID), string(appID), string(buildID))
	if ok {
		writeJSON(w, http.StatusOK, build)
	}
}

func (h *generatedHandler) ListAppBuildLogs(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, buildID generated.BuildId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	if _, ok = h.appBuild(w, r, workspace.ID, string(projectID), string(appID), string(buildID)); !ok {
		return
	}
	items, err := h.server.Store.ListBuildLogs(r.Context(), workspace.ID, string(buildID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not read build logs", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) ListAppReleases(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, params generated.ListAppReleasesParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.Store.ListReleases(r.Context(), workspace.ID, string(projectID), string(appID), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list releases", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateReleaseDeployment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, releaseID generated.ReleaseId, _ generated.CreateReleaseDeploymentParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	payloadHash = scopedBuildPayloadHash(r, payloadHash)
	var input struct {
		Name          string           `json:"name"`
		EnvironmentID string           `json:"environmentId"`
		Replicas      int32            `json:"replicas"`
		Port          int32            `json:"port"`
		Resources     domain.Resources `json:"resources"`
		Probes        domain.Probes    `json:"probes"`
		Exposure      string           `json:"exposure"`
		Slug          string           `json:"slug,omitempty"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	release, err := h.server.Store.FindRelease(r.Context(), workspace.ID, string(projectID), string(appID), string(releaseID))
	if err != nil {
		writeBuildError(w, r, err)
		return
	}
	intent := domain.NormalizeIntent(domain.Intent{Name: input.Name, AppID: string(appID), EnvironmentID: input.EnvironmentID, Image: release.Image, Replicas: input.Replicas, Port: input.Port, Resources: input.Resources, Probes: input.Probes, Exposure: input.Exposure, Slug: input.Slug})
	if err = domain.ValidateIntent(intent, h.server.Config.MaxReplicas, h.server.Config.MaxCPU, h.server.Config.MaxMemory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent", err.Error(), r)
		return
	}
	if !h.server.Config.RegistryAllowed(intent.Image) {
		writeError(w, http.StatusBadRequest, "registry_not_allowed", "image registry is not allowed", r)
		return
	}
	for range 3 {
		deploymentID, idErr := h.server.deploymentID()
		if idErr != nil {
			break
		}
		deployment, operation, _, createErr := h.server.Store.CreateDeploymentForRelease(r.Context(), workspace.ID, actor.ID, deploymentID, string(appID), input.EnvironmentID, release, intent, auth.HashToken(idempotencyKey), payloadHash)
		if errors.Is(createErr, store.ErrPublicIDCollision) {
			continue
		}
		if createErr != nil {
			writeBuildError(w, r, createErr)
			return
		}
		h.server.logAcceptedOperation(r, operation)
		writeJSON(w, http.StatusAccepted, map[string]any{"deployment": deployment, "operation": operation})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a deployment identifier", r)
}

func scopedBuildPayloadHash(r *http.Request, bodyHash []byte) []byte {
	value := append([]byte(r.Method+"\n"+r.URL.EscapedPath()+"\n"), bodyHash...)
	return domain.SHA256(value)
}

func (h *generatedHandler) appBuild(w http.ResponseWriter, r *http.Request, workspaceID int64, projectID, appID, buildID string) (domain.Build, bool) {
	build, err := h.server.Store.FindBuild(r.Context(), workspaceID, buildID)
	if err != nil || build.ProjectPublicID != projectID || build.AppPublicID != appID {
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "storage_failed", "could not read build", r)
		} else {
			writeError(w, http.StatusNotFound, "build_not_found", "build was not found", r)
		}
		return domain.Build{}, false
	}
	return build, true
}

func (h *generatedHandler) appExists(w http.ResponseWriter, r *http.Request, workspaceID int64, projectID, appID string) bool {
	_, err := h.server.Store.FindApp(r.Context(), workspaceID, projectID, appID)
	if err != nil {
		writeBuildError(w, r, err)
		return false
	}
	return true
}

func writeBuildError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "build_resource_not_found", "build resource was not found", r)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "build_conflict", "build request conflicts with existing state", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "build state could not be persisted", r)
	}
}
