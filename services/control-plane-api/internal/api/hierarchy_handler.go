package api

import (
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

type hierarchyInput struct {
	Name string `json:"name"`
}

func (h *generatedHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request, params generated.ListWorkspacesParams) {
	user, ok := h.authorizeUser(w, r, false)
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.Store.ListWorkspaces(r.Context(), user.ID, beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list workspaces", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateWorkspace(w http.ResponseWriter, r *http.Request, _ generated.CreateWorkspaceParams) {
	user, ok := h.authorizeUser(w, r, true)
	if !ok {
		return
	}
	context, err := h.server.Store.AuthorizationContext(r.Context(), user.ID, 0, "Installation", "default")
	if err != nil || !authorization.Allowed(context, authorization.CreateWorkspace) {
		writeError(w, http.StatusForbidden, "permission_denied", "installation administration is required", r)
		return
	}
	idem, payload, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	name, _, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	for range 3 {
		workspaceID, err := domain.NewPublicID("ws")
		if err != nil {
			break
		}
		operationID, err := domain.NewPublicID("op")
		if err != nil {
			break
		}
		workspace, operation, _, err := h.server.Store.CreateWorkspace(r.Context(), user.ID, workspaceID, operationID, name, domain.SHA256([]byte(idem)), payload)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeHierarchyError(w, r, err)
			return
		}
		h.server.logAcceptedOperation(r, operation)
		writeJSON(w, http.StatusAccepted, map[string]any{"workspace": workspace, "operation": operation})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate resource identifiers", r)
}

func (h *generatedHandler) GetWorkspace(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if ok {
		writeJSON(w, http.StatusOK, workspace)
	}
}

func (h *generatedHandler) UpdateWorkspace(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, params generated.UpdateWorkspaceParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	name, _, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	updated, err := h.server.Store.UpdateWorkspace(r.Context(), workspace.ID, int64(params.IfMatch), name)
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *generatedHandler) ListProjects(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, params generated.ListProjectsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.Store.ListProjects(r.Context(), workspace.ID, beforeID, limit, params.IncludeArchived != nil && bool(*params.IncludeArchived))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list projects", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateProject(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	name, nameKey, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	for range 3 {
		publicID, err := domain.NewPublicID("prj")
		if err != nil {
			break
		}
		project, err := h.server.Store.CreateProject(r.Context(), workspace.ID, publicID, name, nameKey)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeHierarchyError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, project)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a project identifier", r)
}

func (h *generatedHandler) GetProject(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	project, err := h.server.Store.FindProject(r.Context(), workspace.ID, string(projectID))
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (h *generatedHandler) UpdateProject(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, params generated.UpdateProjectParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	name, nameKey, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	project, err := h.server.Store.UpdateProject(r.Context(), workspace.ID, string(projectID), int64(params.IfMatch), name, nameKey)
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (h *generatedHandler) ArchiveProject(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, params generated.ArchiveProjectParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	if _, err := h.server.Store.ArchiveProject(r.Context(), workspace.ID, string(projectID), int64(params.IfMatch)); err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) ListEnvironments(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, params generated.ListEnvironmentsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok || !h.projectExists(w, r, workspace.ID, string(projectID)) {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.Store.ListEnvironments(r.Context(), workspace.ID, string(projectID), beforeID, limit, params.IncludeArchived != nil && bool(*params.IncludeArchived))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list environments", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	name, nameKey, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	for range 3 {
		publicID, err := domain.NewPublicID("env")
		if err != nil {
			break
		}
		environment, err := h.server.Store.CreateEnvironment(r.Context(), workspace.ID, string(projectID), publicID, name, nameKey)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeHierarchyError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, environment)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate an environment identifier", r)
}

func (h *generatedHandler) GetEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, environmentID generated.EnvironmentId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	environment, err := h.server.Store.FindEnvironment(r.Context(), workspace.ID, string(projectID), string(environmentID))
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (h *generatedHandler) UpdateEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, environmentID generated.EnvironmentId, params generated.UpdateEnvironmentParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	name, nameKey, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	environment, err := h.server.Store.UpdateEnvironment(r.Context(), workspace.ID, string(projectID), string(environmentID), int64(params.IfMatch), name, nameKey)
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (h *generatedHandler) ArchiveEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, environmentID generated.EnvironmentId, params generated.ArchiveEnvironmentParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	if _, err := h.server.Store.ArchiveEnvironment(r.Context(), workspace.ID, string(projectID), string(environmentID), int64(params.IfMatch)); err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) ListApps(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, params generated.ListAppsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok || !h.projectExists(w, r, workspace.ID, string(projectID)) {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.Store.ListApps(r.Context(), workspace.ID, string(projectID), beforeID, limit, params.IncludeArchived != nil && bool(*params.IncludeArchived))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list apps", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateApp(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	name, nameKey, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	for range 3 {
		publicID, err := domain.NewPublicID("app")
		if err != nil {
			break
		}
		app, err := h.server.Store.CreateApp(r.Context(), workspace.ID, string(projectID), publicID, name, nameKey)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeHierarchyError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, app)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate an app identifier", r)
}

func (h *generatedHandler) GetApp(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	app, err := h.server.Store.FindApp(r.Context(), workspace.ID, string(projectID), string(appID))
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (h *generatedHandler) UpdateApp(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, params generated.UpdateAppParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	name, nameKey, ok := hierarchyName(w, r)
	if !ok {
		return
	}
	app, err := h.server.Store.UpdateApp(r.Context(), workspace.ID, string(projectID), string(appID), int64(params.IfMatch), name, nameKey)
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (h *generatedHandler) ArchiveApp(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, params generated.ArchiveAppParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	if _, err := h.server.Store.ArchiveApp(r.Context(), workspace.ID, string(projectID), string(appID), int64(params.IfMatch)); err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) authorizeUser(w http.ResponseWriter, r *http.Request, mutation bool) (identity.User, bool) {
	userID, csrf, ok := h.server.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return identity.User{}, false
	}
	user, err := h.server.Store.User(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return identity.User{}, false
	}
	if mutation && !h.server.validCSRF(r, csrf) {
		writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
		return identity.User{}, false
	}
	return user, true
}

func (h *generatedHandler) authorizeWorkspace(w http.ResponseWriter, r *http.Request, publicID string, mutation bool) (identity.User, domain.Workspace, bool) {
	user, ok := h.authorizeUser(w, r, mutation)
	if !ok {
		return identity.User{}, domain.Workspace{}, false
	}
	workspace, err := h.server.Store.FindWorkspaceForUser(r.Context(), user.ID, publicID)
	if err != nil {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
		return identity.User{}, domain.Workspace{}, false
	}
	permission := authorization.ReadWorkspace
	resourceType, resourceID := "Workspace", workspace.PublicID
	if mutation {
		permission = mutationPermission(r)
		resourceType, resourceID = authorizationResource(r.URL.Path, workspace.PublicID)
	}
	context, err := h.server.Store.AuthorizationContext(r.Context(), user.ID, workspace.ID, resourceType, resourceID)
	if err != nil || !authorization.Allowed(context, permission) {
		writeError(w, http.StatusForbidden, "permission_denied", "permission is required", r)
		return identity.User{}, domain.Workspace{}, false
	}
	return user, workspace, true
}

func mutationPermission(r *http.Request) authorization.Permission {
	if r.Method == http.MethodPost && (strings.Contains(r.URL.Path, "/builds") || strings.Contains(r.URL.Path, "/deployments")) {
		return authorization.Deploy
	}
	return authorization.EditResources
}

func authorizationResource(path, workspaceID string) (string, string) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	resourceType, resourceID := "Workspace", workspaceID
	for index := 0; index+1 < len(segments); index++ {
		switch segments[index] {
		case "projects":
			resourceType, resourceID = "Project", segments[index+1]
		case "apps":
			if resourceType == "Project" {
				resourceType, resourceID = "App", segments[index+1]
			}
		case "environments":
			if resourceType == "App" {
				candidate := segments[index+1]
				if strings.HasPrefix(candidate, "aev-") {
					resourceType, resourceID = "AppEnvironment", candidate
				}
			}
		}
	}
	return resourceType, resourceID
}

func (h *generatedHandler) projectExists(w http.ResponseWriter, r *http.Request, workspaceID int64, projectID string) bool {
	if _, err := h.server.Store.FindProject(r.Context(), workspaceID, projectID); err != nil {
		writeHierarchyError(w, r, err)
		return false
	}
	return true
}

func hierarchyName(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	var input hierarchyInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return "", "", false
	}
	name, nameKey, err := domain.NormalizeHierarchyName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_name", err.Error(), r)
		return "", "", false
	}
	return name, nameKey, true
}

func hierarchyPage(w http.ResponseWriter, r *http.Request, cursor *generated.Cursor, rawLimit *generated.Limit) (int64, int, bool) {
	beforeID := int64(math.MaxInt64)
	if cursor != nil {
		var err error
		beforeID, err = domain.DecodeCursor(string(*cursor))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is invalid", r)
			return 0, 0, false
		}
	}
	limit := 50
	if rawLimit != nil {
		limit = int(*rawLimit)
		if limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100", r)
			return 0, 0, false
		}
	}
	return beforeID, limit, true
}

func writeHierarchyList(w http.ResponseWriter, items any, nextCursor string) {
	var next any
	if nextCursor != "" {
		next = nextCursor
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func writeHierarchyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrNoRows):
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "resource changed since it was read", r)
	case errors.Is(err, store.ErrNameConflict):
		writeError(w, http.StatusConflict, "name_conflict", "an active resource already uses this name", r)
	case errors.Is(err, store.ErrDependencyConflict):
		writeError(w, http.StatusConflict, "dependency_conflict", "resource has active dependencies", r)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "resource_conflict", "resource changed or has active dependencies", r)
	case errors.Is(err, store.ErrPublicIDCollision):
		writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a resource identifier", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "resource could not be persisted", r)
	}
}
