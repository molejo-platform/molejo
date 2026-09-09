package api

import (
	"errors"
	"math"
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/workspaceprovisioning"
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
	items, nextCursor, err := h.server.store.ListWorkspaces(r.Context(), user.ID, beforeID, limit)
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
	authorizationContext, err := h.server.store.AuthorizationContext(r.Context(), user.ID, 0, "Installation", "default")
	actorAuthorized := err == nil && authorization.Allowed(authorizationContext, authorization.CreateWorkspace)
	idem, payload, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	var input generated.WorkspaceCreateInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	if input.ClusterId == "" {
		writeError(w, http.StatusBadRequest, "cluster_required", "clusterId is required", r)
		return
	}
	name, _, err := domain.NormalizeHierarchyName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_name", err.Error(), r)
		return
	}
	facts, err := h.server.store.WorkspaceProvisioningFacts(r.Context(), input.ClusterId)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "cluster provisioning facts could not be loaded", r)
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
		decision := workspaceprovisioning.Decide(workspaceprovisioning.Input{
			ActorAuthorized: actorAuthorized, ClusterAttached: facts.ClusterAttached,
			CapabilityAvailable: facts.CapabilityAvailable, Consent: facts.Consent,
			Namespace: workspaceID, IdempotencyPresent: idem != "",
		})
		if !decision.Accepted {
			h.server.logger().Warn("workspace provisioning denied", "request_id", requestID(r), "cluster_id", input.ClusterId, "reason_code", decision.Reason)
			writeWorkspaceProvisioningDenial(w, r, decision.Reason)
			return
		}
		var workspace domain.Workspace
		var operation domain.Operation
		workspace, operation, _, err = h.server.store.CreateWorkspaceOnCluster(r.Context(), user.ID, input.ClusterId, workspaceID, operationID, name, domain.SHA256([]byte(idem)), payload)
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

func writeWorkspaceProvisioningDenial(w http.ResponseWriter, r *http.Request, reason workspaceprovisioning.Reason) {
	status := http.StatusConflict
	message := "workspace provisioning request was not admitted"
	if reason == workspaceprovisioning.ReasonActorUnauthorized {
		status, message = http.StatusForbidden, "installation administration is required"
	} else if reason == workspaceprovisioning.ReasonClusterNotAttached {
		status, message = http.StatusNotFound, "cluster is not attached"
	}
	writeError(w, status, string(reason), message, r)
}

func (h *generatedHandler) ListWorkspaceClusters(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	items, err := h.server.store.ListWorkspaceClusters(r.Context(), workspace.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "workspace clusters could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) AttachWorkspaceCluster(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, _ generated.AttachWorkspaceClusterParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	context, err := h.server.store.AuthorizationContext(r.Context(), actor.ID, workspace.ID, "Workspace", workspace.PublicID)
	if err != nil || !authorization.Allowed(context, authorization.ManageWorkspace) {
		writeError(w, http.StatusForbidden, "permission_denied", "workspace management permission is required", r)
		return
	}
	idem, payload, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	var input generated.WorkspaceClusterInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	operationID, err := domain.NewPublicID("op")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate an operation identifier", r)
		return
	}
	binding, operation, _, err := h.server.store.AttachWorkspaceCluster(r.Context(), workspace.ID, actor.ID, input.ClusterId, operationID, domain.SHA256([]byte(idem)), payload)
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	h.server.logAcceptedOperation(r, operation)
	writeJSON(w, http.StatusAccepted, map[string]any{"workspaceCluster": binding, "operation": operation})
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
	updated, err := h.server.store.UpdateWorkspace(r.Context(), workspace.ID, int64(params.IfMatch), name)
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
	items, nextCursor, err := h.server.store.ListProjects(r.Context(), workspace.ID, beforeID, limit, params.IncludeArchived != nil && bool(*params.IncludeArchived))
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
		project, err := h.server.store.CreateProject(r.Context(), workspace.ID, publicID, name, nameKey)
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
	project, err := h.server.store.FindProject(r.Context(), workspace.ID, string(projectID))
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
	project, err := h.server.store.UpdateProject(r.Context(), workspace.ID, string(projectID), int64(params.IfMatch), name, nameKey)
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
	if _, err := h.server.store.ArchiveProject(r.Context(), workspace.ID, string(projectID), int64(params.IfMatch)); err != nil {
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
	items, nextCursor, err := h.server.store.ListEnvironments(r.Context(), workspace.ID, string(projectID), beforeID, limit, params.IncludeArchived != nil && bool(*params.IncludeArchived))
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
		environment, err := h.server.store.CreateEnvironment(r.Context(), workspace.ID, string(projectID), publicID, name, nameKey)
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
	environment, err := h.server.store.FindEnvironment(r.Context(), workspace.ID, string(projectID), string(environmentID))
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
	environment, err := h.server.store.UpdateEnvironment(r.Context(), workspace.ID, string(projectID), string(environmentID), int64(params.IfMatch), name, nameKey)
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
	if _, err := h.server.store.ArchiveEnvironment(r.Context(), workspace.ID, string(projectID), string(environmentID), int64(params.IfMatch)); err != nil {
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
	items, nextCursor, err := h.server.store.ListApps(r.Context(), workspace.ID, string(projectID), beforeID, limit, params.IncludeArchived != nil && bool(*params.IncludeArchived))
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
		app, err := h.server.store.CreateApp(r.Context(), workspace.ID, string(projectID), publicID, name, nameKey)
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
	app, err := h.server.store.FindApp(r.Context(), workspace.ID, string(projectID), string(appID))
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
	app, err := h.server.store.UpdateApp(r.Context(), workspace.ID, string(projectID), string(appID), int64(params.IfMatch), name, nameKey)
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
	if _, err := h.server.store.ArchiveApp(r.Context(), workspace.ID, string(projectID), string(appID), int64(params.IfMatch)); err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) projectExists(w http.ResponseWriter, r *http.Request, workspaceID int64, projectID string) bool {
	if _, err := h.server.store.FindProject(r.Context(), workspaceID, projectID); err != nil {
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
