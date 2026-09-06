package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListAppServiceAccounts(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ManageAutomation)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	items, err := h.server.store.ListServiceAccounts(r.Context(), workspace.ID, string(projectID), string(appID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list service accounts", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) CreateAppServiceAccount(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageAutomation)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	var input struct {
		Name                     string    `json:"name"`
		DeploymentEnvironmentIDs *[]string `json:"deploymentEnvironmentIds"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	name, err := automation.NormalizeName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "service_account_invalid", "service account name or environment scope is invalid", r)
		return
	}
	if input.DeploymentEnvironmentIDs == nil {
		writeError(w, http.StatusBadRequest, "service_account_invalid", "service account name or environment scope is invalid", r)
		return
	}
	if err = automation.ValidateEnvironmentScope(*input.DeploymentEnvironmentIDs); err != nil {
		writeError(w, http.StatusBadRequest, "service_account_invalid", "service account name or environment scope is invalid", r)
		return
	}
	for range 3 {
		serviceAccountID, idErr := domain.NewPublicID("svc")
		if idErr != nil {
			break
		}
		event := h.server.auditEvent(r, "service_account.create", "ServiceAccount", serviceAccountID, audit.Succeeded)
		account, createErr := h.server.store.CreateServiceAccount(r.Context(), store.CreateServiceAccountParams{
			WorkspaceID:              workspace.ID,
			UserID:                   actor.ID,
			ProjectPublicID:          string(projectID),
			AppPublicID:              string(appID),
			ServiceAccountPublicID:   serviceAccountID,
			Name:                     name,
			DeploymentEnvironmentIDs: *input.DeploymentEnvironmentIDs,
			AuditEvent:               event,
		})
		if errors.Is(createErr, store.ErrPublicIDCollision) {
			continue
		}
		if createErr != nil {
			writeAutomationError(w, r, createErr)
			return
		}
		writeJSON(w, http.StatusCreated, account)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a service account identifier", r)
}

func (h *generatedHandler) RevokeAppServiceAccount(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, serviceAccountID generated.ServiceAccountId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageAutomation)
	if !ok {
		return
	}
	event := h.server.auditEvent(r, "service_account.revoke", "ServiceAccount", string(serviceAccountID), audit.Succeeded)
	if err := h.server.store.RevokeServiceAccount(r.Context(), workspace.ID, actor.ID, string(projectID), string(appID), string(serviceAccountID), event); err != nil {
		writeAutomationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
