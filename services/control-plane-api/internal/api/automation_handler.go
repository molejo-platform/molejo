package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
	releasecontract "github.com/molejo-platform/molejo/services/control-plane-api/internal/release"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

const (
	defaultAutomationCredentialTTL = 90 * 24 * time.Hour
	maximumAutomationCredentialTTL = 365 * 24 * time.Hour
)

func (h *generatedHandler) RegisterAppRelease(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, _ generated.RegisterAppReleaseParams) {
	actor, workspace, ok := h.authorizeAutomation(w, r, string(workspaceID), string(projectID), string(appID), "", automation.PermissionReleaseWrite)
	if !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	var command releasecontract.RegisterCommand
	if err := decodeJSON(r, &command); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	if err := releasecontract.Validate(command); err != nil {
		writeError(w, http.StatusBadRequest, "release_invalid", err.Error(), r)
		return
	}
	if !h.server.config.RegistryAllowed(command.Artifact.Reference) {
		writeError(w, http.StatusBadRequest, "registry_not_allowed", "artifact registry is not allowed", r)
		return
	}
	for range 3 {
		releaseID, err := domain.NewPublicID("rel")
		if err != nil {
			break
		}
		event := h.server.auditEvent(r, "release.register", "Release", releaseID, audit.Succeeded)
		item, replay, err := h.server.store.RegisterExternalRelease(r.Context(), actor, workspace.ID, string(projectID), string(appID), releaseID, command, auth.HashToken(idempotencyKey), scopedBuildPayloadHash(r, payloadHash), event)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeAutomationError(w, r, err)
			return
		}
		status := http.StatusCreated
		if replay {
			status = http.StatusOK
		}
		writeJSON(w, status, item)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a Release identifier", r)
}

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
	if err != nil || input.DeploymentEnvironmentIDs == nil || !validEnvironmentScope(*input.DeploymentEnvironmentIDs) {
		writeError(w, http.StatusBadRequest, "service_account_invalid", "service account name or environment scope is invalid", r)
		return
	}
	for range 3 {
		serviceAccountID, idErr := domain.NewPublicID("svc")
		if idErr != nil {
			break
		}
		event := h.server.auditEvent(r, "service_account.create", "ServiceAccount", serviceAccountID, audit.Succeeded)
		account, createErr := h.server.store.CreateServiceAccount(r.Context(), workspace.ID, actor.ID, string(projectID), string(appID), serviceAccountID, name, *input.DeploymentEnvironmentIDs, event)
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
	now := time.Now()
	expiresAt := now.Add(defaultAutomationCredentialTTL).UTC()
	if input.ExpiresAt != nil {
		expiresAt = input.ExpiresAt.UTC()
	}
	if expiresAt.Before(now.Add(time.Minute)) || expiresAt.After(now.Add(maximumAutomationCredentialTTL)) {
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
		created, createErr := h.server.store.CreateServiceAccountToken(r.Context(), workspace.ID, actor.ID, string(projectID), string(appID), string(serviceAccountID), tokenID, auth.HashToken(token), expiresAt, event)
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

func (h *generatedHandler) authorizeAutomation(w http.ResponseWriter, r *http.Request, workspaceID, projectID, appID, appEnvironmentID, permission string) (principal.Principal, domain.Workspace, bool) {
	token, valid := automationBearerToken(r.Header.Get("Authorization"))
	if !valid {
		writeError(w, http.StatusUnauthorized, "automation_unauthenticated", "valid service account bearer token is required", r)
		return principal.Principal{}, domain.Workspace{}, false
	}
	actor, err := h.server.store.AuthenticateServiceAccount(r.Context(), auth.HashToken(token))
	if err != nil {
		writeAutomationError(w, r, err)
		return principal.Principal{}, domain.Workspace{}, false
	}
	workspace, err := h.server.store.AuthorizeServiceAccount(r.Context(), actor, workspaceID, projectID, appID, appEnvironmentID, permission)
	if err != nil {
		writeAutomationError(w, r, err)
		return principal.Principal{}, domain.Workspace{}, false
	}
	return actor, workspace, true
}

func automationBearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	returnValue := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && len(parts[1]) >= 32 && len(parts[1]) <= 512 {
		returnValue = parts[1]
	}
	return returnValue, returnValue != ""
}

func validEnvironmentScope(values []string) bool {
	if len(values) > 20 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if domain.ValidateAppEnvironmentID(value) != nil {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func writeAutomationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrAutomationAuthentication):
		writeError(w, http.StatusUnauthorized, "automation_unauthenticated", "valid service account bearer token is required", r)
	case errors.Is(err, store.ErrAutomationAuthorization):
		writeError(w, http.StatusForbidden, "automation_forbidden", "service account scope does not allow this action", r)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
	case errors.Is(err, store.ErrNameConflict):
		writeError(w, http.StatusConflict, "service_account_name_conflict", "a service account with this name already exists", r)
	case errors.Is(err, store.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with a different request", r)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "resource_conflict", "resource state conflicts with this request", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "automation request could not be completed", r)
	}
}
