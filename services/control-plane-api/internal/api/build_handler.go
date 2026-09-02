package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/githubapp"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
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
	items, nextCursor, err := h.server.store.ListBuilds(r.Context(), workspace.ID, string(projectID), string(appID), beforeID, limit)
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
	var input struct {
		AppEnvironmentID string `json:"appEnvironmentId"`
		CommitSHA        string `json:"commitSha"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	if err := domain.ValidateAppEnvironmentID(input.AppEnvironmentID); err != nil {
		writeError(w, http.StatusBadRequest, "app_environment_invalid", err.Error(), r)
		return
	}
	if existing, found, err := h.server.store.FindBuildByIdempotency(r.Context(), workspace.ID, actor.ID, idempotencyHash, payloadHash); err != nil {
		writeBuildError(w, r, err)
		return
	} else if found {
		writeJSON(w, http.StatusAccepted, existing)
		return
	}
	if !h.githubAvailable(w, r) {
		return
	}
	source, err := h.server.store.GitHubBuildSource(r.Context(), workspace.ID, string(projectID), string(appID), input.AppEnvironmentID)
	if err != nil {
		writeBuildError(w, r, err)
		return
	}
	branch := source.SourceBranch
	ref := branch
	if input.CommitSHA != "" {
		if err = domain.ValidateCommitSHA(input.CommitSHA); err != nil {
			writeError(w, http.StatusBadRequest, "commit_invalid", err.Error(), r)
			return
		}
		ref = input.CommitSHA
	}
	metadata, err := h.server.github.Commit(r.Context(), source.InstallationExternalID, source.RepositoryID, ref)
	if err != nil {
		if errors.Is(err, githubapp.ErrNotFound) {
			if input.CommitSHA != "" {
				writeError(w, http.StatusBadRequest, "commit_not_found", "commit was not found in the repository", r)
			} else {
				writeError(w, http.StatusBadRequest, "branch_not_found", "branch was not found in the repository", r)
			}
			return
		}
		h.server.logger().Warn("resolve GitHub build commit", "request_id", requestID(r), "app_id", source.AppPublicID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_unavailable", "GitHub commit could not be resolved", r)
		return
	}
	for range 3 {
		publicID, idErr := domain.NewPublicID("bld")
		if idErr != nil {
			break
		}
		build, _, createErr := h.server.store.CreateBuildWithMetadata(r.Context(), workspace.ID, actor.ID, publicID, string(projectID), string(appID), input.AppEnvironmentID, branch, metadata, idempotencyHash, payloadHash)
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
	items, err := h.server.store.ListBuildLogs(r.Context(), workspace.ID, string(buildID))
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
	items, nextCursor, err := h.server.store.ListReleases(r.Context(), workspace.ID, string(projectID), string(appID), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list releases", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func scopedBuildPayloadHash(r *http.Request, bodyHash []byte) []byte {
	value := append([]byte(r.Method+"\n"+r.URL.EscapedPath()+"\n"), bodyHash...)
	return domain.SHA256(value)
}

func (h *generatedHandler) appBuild(w http.ResponseWriter, r *http.Request, workspaceID int64, projectID, appID, buildID string) (domain.Build, bool) {
	build, err := h.server.store.FindBuild(r.Context(), workspaceID, buildID)
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
	_, err := h.server.store.FindApp(r.Context(), workspaceID, projectID, appID)
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
