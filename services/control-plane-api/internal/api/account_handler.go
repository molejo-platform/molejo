package api

import (
	"net/http"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
)

const passwordResetTTL = 15 * time.Minute

func (h *generatedHandler) GetAuthenticationCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"password": true, "totp": h.server.config.TOTPEnabled, "passkey": false})
}

func (h *generatedHandler) GetOwnProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authorizeUser(w, r, false)
	if ok {
		writeJSON(w, http.StatusOK, user)
	}
}

func (h *generatedHandler) UpdateOwnProfile(w http.ResponseWriter, r *http.Request, params generated.UpdateOwnProfileParams) {
	user, ok := h.authorizeUser(w, r, true)
	if !ok {
		return
	}
	var input generated.ProfileInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	displayName, err := identity.NormalizeDisplayName(input.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "profile_invalid", err.Error(), r)
		return
	}
	event := h.server.auditEvent(r, "identity.profile.update", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &user.ID
	updated, err := h.server.store.UpdateOwnProfile(r.Context(), user.ID, int64(params.IfMatch), displayName, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *generatedHandler) ChangeOwnPassword(w http.ResponseWriter, r *http.Request, _ generated.ChangeOwnPasswordParams) {
	user, ok := h.authorizeUser(w, r, true)
	if !ok {
		return
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	_, currentHash, err := h.server.store.AuthenticateUser(r.Context(), user.Username)
	if err != nil || !auth.VerifyPassword(input.CurrentPassword, currentHash) {
		writeError(w, http.StatusBadRequest, "current_password_invalid", "current password is invalid", r)
		return
	}
	if err = auth.ValidatePassword(input.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, "password_invalid", err.Error(), r)
		return
	}
	passwordHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_failed", "password could not be changed", r)
		return
	}
	event := h.server.auditEvent(r, "identity.password.change", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &user.ID
	if err = h.server.store.ChangePassword(r.Context(), user.ID, passwordHash, event); err != nil {
		writeError(w, http.StatusInternalServerError, "password_failed", "password could not be changed", r)
		return
	}
	h.server.clearSessionCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) ListOwnSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.server.sessionPrincipal(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	items, err := h.server.store.ListUserSessions(r.Context(), principal.UserID, principal.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "sessions could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) RevokeOwnSession(w http.ResponseWriter, r *http.Request, sessionID generated.UserSessionId) {
	principal, ok := h.server.sessionPrincipal(r)
	if !ok || !h.server.validCSRF(r, principal.CSRFHash) {
		writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
		return
	}
	event := h.server.auditEvent(r, "authentication.session.revoke", "Session", string(sessionID), audit.Succeeded)
	event.ActorUserID = &principal.UserID
	event.SessionID = &principal.SessionID
	if err := h.server.store.RevokeUserSession(r.Context(), principal.UserID, string(sessionID), event); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	if principal.PublicID == string(sessionID) {
		h.server.clearSessionCookies(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var input generated.PasswordResetRequest
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	username, _ := identity.NormalizeUsername(input.Username)
	_ = h.server.recordAudit(r, audit.Event{Action: "identity.password_reset.request", TargetType: "User", TargetPublicID: username, Outcome: audit.Succeeded})
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

func (h *generatedHandler) VerifyPasswordReset(w http.ResponseWriter, r *http.Request) {
	if len(h.server.passwordResetKey) < 32 {
		writeError(w, http.StatusServiceUnavailable, "password_reset_unavailable", "password reset is not configured", r)
		return
	}
	var input struct {
		Username string `json:"username"`
		Code     string `json:"code"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	username, err := identity.NormalizeUsername(input.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, "reset_code_invalid", "reset code is invalid or expired", r)
		return
	}
	ipKey := auth.HashToken("reset-ip:" + h.server.remoteIP(r))
	userKey := auth.HashToken("reset-user:" + username)
	ipAllowed, ipErr := h.server.store.AuthenticationAllowed(r.Context(), ipKey)
	userAllowed, userErr := h.server.store.AuthenticationAllowed(r.Context(), userKey)
	if ipErr != nil || userErr != nil {
		writeError(w, http.StatusServiceUnavailable, "password_reset_unavailable", "password reset is temporarily unavailable", r)
		return
	}
	if !ipAllowed || !userAllowed {
		writeError(w, http.StatusTooManyRequests, "password_reset_rate_limited", "too many reset attempts", r)
		return
	}
	user, ticket, err := h.server.store.VerifyPasswordResetGrant(r.Context(), username, auth.HashResetCode(h.server.passwordResetKey, input.Code))
	if err != nil {
		_ = h.server.store.RecordAuthenticationFailure(r.Context(), ipKey)
		_ = h.server.store.RecordAuthenticationFailure(r.Context(), userKey)
		_ = h.server.recordAudit(r, audit.Event{Action: "identity.password_reset.verify", TargetType: "User", Outcome: audit.Failed, Reason: "invalid_code"})
		writeError(w, http.StatusBadRequest, "reset_code_invalid", "reset code is invalid or expired", r)
		return
	}
	_ = h.server.store.ClearAuthenticationFailures(r.Context(), ipKey)
	_ = h.server.store.ClearAuthenticationFailures(r.Context(), userKey)
	_ = h.server.recordAudit(r, audit.Event{Action: "identity.password_reset.verify", TargetType: "User", TargetPublicID: user.PublicID, Outcome: audit.Succeeded})
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
}

func (h *generatedHandler) CompletePasswordReset(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username    string `json:"username"`
		Ticket      string `json:"ticket"`
		NewPassword string `json:"newPassword"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	username, err := identity.NormalizeUsername(input.Username)
	if err != nil || auth.ValidatePassword(input.NewPassword) != nil {
		writeError(w, http.StatusBadRequest, "password_reset_invalid", "password reset request is invalid", r)
		return
	}
	passwordHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_reset_failed", "password could not be reset", r)
		return
	}
	event := h.server.auditEvent(r, "identity.password_reset.complete", "User", "", audit.Succeeded)
	if err = h.server.store.CompletePasswordReset(r.Context(), username, input.Ticket, passwordHash, event); err != nil {
		writeError(w, http.StatusBadRequest, "password_reset_invalid", "password reset request is invalid", r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
