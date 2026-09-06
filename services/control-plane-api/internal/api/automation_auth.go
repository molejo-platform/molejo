package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func usesAutomationAuthentication(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("Authorization")) != ""
}

func (h *generatedHandler) authenticateAutomation(w http.ResponseWriter, r *http.Request) (principal.Principal, bool) {
	token, valid := automationBearerToken(r.Header.Get("Authorization"))
	if !valid {
		writeError(w, http.StatusUnauthorized, "automation_unauthenticated", "valid service account bearer token is required", r)
		return principal.Principal{}, false
	}
	actor, err := h.server.store.AuthenticateServiceAccount(r.Context(), auth.HashToken(token))
	if err != nil {
		writeAutomationError(w, r, err)
		return principal.Principal{}, false
	}
	return actor, true
}

func (h *generatedHandler) authorizeAutomation(w http.ResponseWriter, r *http.Request, workspaceID, projectID, appID, appEnvironmentID string, permission automation.Permission) (principal.Principal, domain.Workspace, bool) {
	actor, ok := h.authenticateAutomation(w, r)
	if !ok {
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
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) < 32 || len(parts[1]) > 512 {
		return "", false
	}
	return parts[1], true
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
