package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

const userInvitationTTL = 24 * time.Hour

func (h *generatedHandler) UpdateUserInstallationRole(w http.ResponseWriter, r *http.Request, userID generated.UserId, params generated.UpdateUserInstallationRoleParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageUsers)
	if !ok {
		return
	}
	var input generated.InstallationRoleInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "installation_role_invalid", "installation role is invalid", r)
		return
	}
	event := h.server.auditEvent(r, "authorization.installation_role.update", "User", string(userID), audit.Succeeded)
	event.ActorUserID = &administrator.ID
	updated, err := h.server.store.SetInstallationAdministrator(r.Context(), string(userID), input.Administrator, int64(params.IfMatch), event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *generatedHandler) CreateUserInvitation(w http.ResponseWriter, r *http.Request, userID generated.UserId) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageUsers)
	if !ok {
		return
	}
	for range 3 {
		publicID, idErr := domain.NewPublicID("uin")
		token, tokenErr := h.server.newToken(32)
		if idErr != nil || tokenErr != nil {
			break
		}
		expiresAt := time.Now().Add(userInvitationTTL)
		event := h.server.auditEvent(r, "identity.user.invitation.create", "User", string(userID), audit.Succeeded)
		event.ActorUserID = &administrator.ID
		err := h.server.store.CreateUserInvitation(r.Context(), string(userID), store.UserInvitation{PublicID: publicID, TokenHash: auth.HashToken(token), ExpiresAt: expiresAt}, administrator.ID, event)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeIdentityError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"token": token, "expiresAt": expiresAt})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate an invitation identifier", r)
}

func (h *generatedHandler) AcceptUserInvitation(w http.ResponseWriter, r *http.Request, _ generated.AcceptUserInvitationParams) {
	if !h.server.originAllowed(r) {
		writeError(w, http.StatusForbidden, "origin_forbidden", "request origin is not allowed", r)
		return
	}
	var input generated.UserInvitationAcceptance
	if decodeJSON(r, &input) != nil || input.Token == nil || input.Password == nil || auth.ValidatePassword(*input.Password) != nil {
		writeError(w, http.StatusBadRequest, "invitation_invalid", "invitation or password is invalid", r)
		return
	}
	passwordHash, err := auth.HashPassword(*input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invitation_failed", "invitation could not be accepted", r)
		return
	}
	event := h.server.auditEvent(r, "identity.user.invitation.accept", "User", "", audit.Succeeded)
	if _, err = h.server.store.AcceptUserInvitation(r.Context(), auth.HashToken(*input.Token), passwordHash, event); err != nil {
		if errors.Is(err, store.ErrInvitationInvalid) {
			writeError(w, http.StatusBadRequest, "invitation_invalid", "invitation is invalid or expired", r)
			return
		}
		writeIdentityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) ListInstallationAuditEvents(w http.ResponseWriter, r *http.Request, params generated.ListInstallationAuditEventsParams) {
	if _, ok := h.authorizeInstallation(w, r, false, authorization.ManageUsers); !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, next, err := h.server.store.ListInstallationAuditEvents(r.Context(), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "installation audit events could not be listed", r)
		return
	}
	writeHierarchyList(w, items, next)
}
