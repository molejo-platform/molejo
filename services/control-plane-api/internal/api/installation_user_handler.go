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
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListUsers(w http.ResponseWriter, r *http.Request, params generated.ListUsersParams) {
	_, ok := h.authorizeInstallation(w, r, false, authorization.ManageUsers)
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, next, err := h.server.store.ListInstallationUsers(r.Context(), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "users could not be listed", r)
		return
	}
	writeHierarchyList(w, items, next)
}

func (h *generatedHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageUsers)
	if !ok {
		return
	}
	var input generated.UserCreateInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	username, usernameErr := identity.NormalizeUsername(input.Username)
	displayName, displayNameErr := identity.NormalizeDisplayName(input.DisplayName)
	if usernameErr != nil || displayNameErr != nil {
		writeError(w, http.StatusBadRequest, "user_invalid", "username or display name is invalid", r)
		return
	}
	for range 3 {
		publicID, idErr := domain.NewPublicID("usr")
		if idErr != nil {
			break
		}
		invitationID, idErr := domain.NewPublicID("uin")
		if idErr != nil {
			break
		}
		token, tokenErr := h.server.newToken(32)
		if tokenErr != nil {
			break
		}
		expiresAt := time.Now().Add(userInvitationTTL)
		installationAdministrator := input.InstallationAdministrator != nil && *input.InstallationAdministrator
		event := h.server.auditEvent(r, "identity.user.create", "User", publicID, audit.Succeeded)
		event.ActorUserID = &administrator.ID
		invitation := store.UserInvitation{PublicID: invitationID, TokenHash: auth.HashToken(token), ExpiresAt: expiresAt}
		created, createErr := h.server.store.CreateInvitedUser(r.Context(), identity.User{PublicID: publicID, Username: username, DisplayName: displayName}, installationAdministrator, invitation, administrator.ID, event)
		if errors.Is(createErr, store.ErrPublicIDCollision) {
			continue
		}
		if createErr != nil {
			writeIdentityError(w, r, createErr)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"user": created, "token": token, "expiresAt": expiresAt})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a user identifier", r)
}

func (h *generatedHandler) UpdateUserStatus(w http.ResponseWriter, r *http.Request, userID generated.UserId, params generated.UpdateUserStatusParams) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageUsers)
	if !ok {
		return
	}
	var input generated.UserStatusInput
	if decodeJSON(r, &input) != nil || !identity.ValidAdministrativeStatus(string(input.Status)) {
		writeError(w, http.StatusBadRequest, "user_status_invalid", "user status is invalid", r)
		return
	}
	event := h.server.auditEvent(r, "identity.user.status.update", "User", string(userID), audit.Succeeded)
	event.ActorUserID = &administrator.ID
	updated, err := h.server.store.SetUserStatus(r.Context(), string(userID), string(input.Status), int64(params.IfMatch), event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	installationUser, err := h.server.store.FindInstallationUser(r.Context(), updated.PublicID)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, installationUser)
}

func (h *generatedHandler) CreatePasswordResetGrant(w http.ResponseWriter, r *http.Request, userID generated.UserId) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageUsers)
	if !ok {
		return
	}
	if len(h.server.passwordResetKey) < 32 {
		writeError(w, http.StatusServiceUnavailable, "password_reset_unavailable", "password reset is not configured", r)
		return
	}
	user, err := h.server.store.FindUser(r.Context(), string(userID))
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	if user.Status != identity.StatusActive {
		writeError(w, http.StatusConflict, "user_inactive", "password reset requires an active user", r)
		return
	}
	code, err := auth.NewResetCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_reset_failed", "reset code could not be created", r)
		return
	}
	publicID, err := domain.NewPublicID("prg")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_reset_failed", "reset code could not be created", r)
		return
	}
	expiresAt := time.Now().Add(passwordResetTTL)
	event := h.server.auditEvent(r, "identity.password_reset.grant.create", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &administrator.ID
	grant := store.PasswordResetGrant{PublicID: publicID, UserID: user.ID, CodeHash: auth.HashResetCode(h.server.passwordResetKey, code), ExpiresAt: expiresAt}
	if err = h.server.store.CreatePasswordResetGrant(r.Context(), grant, administrator.ID, event); err != nil {
		writeError(w, http.StatusInternalServerError, "password_reset_failed", "reset code could not be created", r)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expiresAt": expiresAt})
}
