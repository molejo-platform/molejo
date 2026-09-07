package api

import (
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
)

func (h *generatedHandler) GetEffectiveCapabilities(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, params generated.GetEffectiveCapabilitiesParams) {
	user, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	resourceType := string(params.ResourceType)
	exists, err := h.server.store.WorkspaceResourceExists(r.Context(), workspace.ID, resourceType, params.ResourceId)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "resource capabilities could not be loaded", r)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
		return
	}
	context, err := h.server.store.AuthorizationContext(r.Context(), user.ID, workspace.ID, resourceType, params.ResourceId)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "resource capabilities could not be loaded", r)
		return
	}
	capabilities := authorization.EffectiveCapabilities(context)
	writeJSON(w, http.StatusOK, generated.EffectiveCapabilities{
		ReadWorkspace:    capabilities.ReadWorkspace,
		ManageWorkspace:  capabilities.ManageWorkspace,
		ManageMembers:    capabilities.ManageMembers,
		ManageGroups:     capabilities.ManageGroups,
		ReadAudit:        capabilities.ReadAudit,
		ManageAutomation: capabilities.ManageAutomation,
		EditResources:    capabilities.EditResources,
		Deploy:           capabilities.Deploy,
	})
}
