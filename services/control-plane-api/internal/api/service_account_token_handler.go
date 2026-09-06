package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListAppServiceAccountTokens(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, serviceAccountID generated.ServiceAccountId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ManageAutomation)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	items, err := h.server.store.ListServiceAccountTokens(r.Context(), workspace.ID, string(projectID), string(appID), string(serviceAccountID))
	if err != nil {
		writeAutomationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) CreateAppServiceAccountToken(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, serviceAccountID generated.ServiceAccountId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageAutomation)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	var input struct {
		ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	expiresAt, err := automation.ResolveCredentialExpiry(time.Now(), input.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "credential_expiry_invalid", "credential expiry must be between one minute and 365 days", r)
		return
	}
	for range 3 {
		tokenID, idErr := domain.NewPublicID("sat")
		if idErr != nil {
			break
		}
		token, tokenErr := h.server.newToken(32)
		if tokenErr != nil {
			break
		}
		event := h.server.auditEvent(r, "service_account_token.create", "ServiceAccountToken", tokenID, audit.Succeeded)
		created, createErr := h.server.store.CreateServiceAccountToken(r.Context(), store.CreateServiceAccountTokenParams{
			WorkspaceID:            workspace.ID,
			UserID:                 actor.ID,
			ProjectPublicID:        string(projectID),
			AppPublicID:            string(appID),
			ServiceAccountPublicID: string(serviceAccountID),
			TokenPublicID:          tokenID,
			TokenHash:              auth.HashToken(token),
			ExpiresAt:              expiresAt,
			AuditEvent:             event,
		})
		if errors.Is(createErr, store.ErrPublicIDCollision) {
			continue
		}
		if createErr != nil {
			writeAutomationError(w, r, createErr)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, automation.Credential{TokenID: created.PublicID, Token: token, ExpiresAt: created.ExpiresAt})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "credential_generation_failed", "could not create service account credential", r)
}

func (h *generatedHandler) RevokeAppServiceAccountToken(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, serviceAccountID generated.ServiceAccountId, tokenID generated.ServiceAccountTokenId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageAutomation)
	if !ok {
		return
	}
	event := h.server.auditEvent(r, "service_account_token.revoke", "ServiceAccountToken", string(tokenID), audit.Succeeded)
	if err := h.server.store.RevokeServiceAccountToken(r.Context(), workspace.ID, actor.ID, string(projectID), string(appID), string(serviceAccountID), string(tokenID), event); err != nil {
		writeAutomationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
