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

const passwordResetTTL = 15 * time.Minute

func (h *generatedHandler) GetAuthenticationCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"password": true, "totp": h.server.Config.TOTPEnabled, "passkey": false})
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
	updated, err := h.server.Store.UpdateOwnProfile(r.Context(), user.ID, int64(params.IfMatch), displayName, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *generatedHandler) ChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
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
	_, currentHash, err := h.server.Store.AuthenticateUser(r.Context(), user.Username)
	if err != nil || !auth.VerifyPassword(input.CurrentPassword, currentHash) {
		writeError(w, http.StatusUnauthorized, "current_password_invalid", "current password is invalid", r)
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
	if err = h.server.Store.ChangePassword(r.Context(), user.ID, passwordHash, event); err != nil {
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
	items, err := h.server.Store.ListUserSessions(r.Context(), principal.UserID, principal.SessionID)
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
	if err := h.server.Store.RevokeUserSession(r.Context(), principal.UserID, string(sessionID), event); err != nil {
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
	if len(h.server.PasswordResetKey) < 32 {
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
	ipAllowed, ipErr := h.server.Store.AuthenticationAllowed(r.Context(), ipKey)
	userAllowed, userErr := h.server.Store.AuthenticationAllowed(r.Context(), userKey)
	if ipErr != nil || userErr != nil {
		writeError(w, http.StatusServiceUnavailable, "password_reset_unavailable", "password reset is temporarily unavailable", r)
		return
	}
	if !ipAllowed || !userAllowed {
		writeError(w, http.StatusTooManyRequests, "password_reset_rate_limited", "too many reset attempts", r)
		return
	}
	user, ticket, err := h.server.Store.VerifyPasswordResetGrant(r.Context(), username, auth.HashResetCode(h.server.PasswordResetKey, input.Code))
	if err != nil {
		_ = h.server.Store.RecordAuthenticationFailure(r.Context(), ipKey)
		_ = h.server.Store.RecordAuthenticationFailure(r.Context(), userKey)
		_ = h.server.recordAudit(r, audit.Event{Action: "identity.password_reset.verify", TargetType: "User", Outcome: audit.Failed, Reason: "invalid_code"})
		writeError(w, http.StatusBadRequest, "reset_code_invalid", "reset code is invalid or expired", r)
		return
	}
	_ = h.server.Store.ClearAuthenticationFailures(r.Context(), ipKey)
	_ = h.server.Store.ClearAuthenticationFailures(r.Context(), userKey)
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
	if err = h.server.Store.CompletePasswordReset(r.Context(), username, input.Ticket, passwordHash, event); err != nil {
		writeError(w, http.StatusBadRequest, "password_reset_invalid", "password reset request is invalid", r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) ListUsers(w http.ResponseWriter, r *http.Request, params generated.ListUsersParams) {
	_, ok := h.authorizeInstallation(w, r, false, authorization.ManageUsers)
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, next, err := h.server.Store.ListUsers(r.Context(), beforeID, limit)
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
	var input struct {
		Username                  string `json:"username"`
		DisplayName               string `json:"displayName"`
		Password                  string `json:"password"`
		InstallationAdministrator bool   `json:"installationAdministrator"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	username, usernameErr := identity.NormalizeUsername(input.Username)
	displayName, displayNameErr := identity.NormalizeDisplayName(input.DisplayName)
	passwordErr := auth.ValidatePassword(input.Password)
	if usernameErr != nil || displayNameErr != nil || passwordErr != nil {
		writeError(w, http.StatusBadRequest, "user_invalid", "username, display name, or password is invalid", r)
		return
	}
	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user_create_failed", "user could not be created", r)
		return
	}
	for range 3 {
		publicID, idErr := domain.NewPublicID("usr")
		if idErr != nil {
			break
		}
		event := h.server.auditEvent(r, "identity.user.create", "User", publicID, audit.Succeeded)
		event.ActorUserID = &administrator.ID
		created, createErr := h.server.Store.CreateUser(r.Context(), identity.User{PublicID: publicID, Username: username, DisplayName: displayName}, passwordHash, input.InstallationAdministrator, event)
		if errors.Is(createErr, store.ErrPublicIDCollision) {
			continue
		}
		if createErr != nil {
			writeIdentityError(w, r, createErr)
			return
		}
		writeJSON(w, http.StatusCreated, created)
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
	if decodeJSON(r, &input) != nil || !identity.ValidStatus(string(input.Status)) {
		writeError(w, http.StatusBadRequest, "user_status_invalid", "user status is invalid", r)
		return
	}
	event := h.server.auditEvent(r, "identity.user.status.update", "User", string(userID), audit.Succeeded)
	event.ActorUserID = &administrator.ID
	updated, err := h.server.Store.SetUserStatus(r.Context(), string(userID), string(input.Status), int64(params.IfMatch), event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *generatedHandler) CreatePasswordResetGrant(w http.ResponseWriter, r *http.Request, userID generated.UserId) {
	administrator, ok := h.authorizeInstallation(w, r, true, authorization.ManageUsers)
	if !ok {
		return
	}
	if len(h.server.PasswordResetKey) < 32 {
		writeError(w, http.StatusServiceUnavailable, "password_reset_unavailable", "password reset is not configured", r)
		return
	}
	user, err := h.server.Store.FindUser(r.Context(), string(userID))
	if err != nil {
		writeIdentityError(w, r, err)
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
	grant := store.PasswordResetGrant{PublicID: publicID, UserID: user.ID, CodeHash: auth.HashResetCode(h.server.PasswordResetKey, code), ExpiresAt: expiresAt}
	if err = h.server.Store.CreatePasswordResetGrant(r.Context(), grant, administrator.ID, event); err != nil {
		writeError(w, http.StatusInternalServerError, "password_reset_failed", "reset code could not be created", r)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expiresAt": expiresAt})
}

func (h *generatedHandler) ListWorkspaceMembers(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	items, err := h.server.Store.ListWorkspaceMemberships(r.Context(), workspace.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "members could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) CreateWorkspaceMember(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageMembers)
	if !ok {
		return
	}
	var input struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Status   string `json:"status"`
	}
	if decodeJSON(r, &input) != nil || !validMembership(input.Role, input.Status) {
		writeError(w, http.StatusBadRequest, "membership_invalid", "membership is invalid", r)
		return
	}
	username, err := identity.NormalizeUsername(input.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, "membership_invalid", "membership is invalid", r)
		return
	}
	user, err := h.server.Store.FindUserByUsername(r.Context(), username)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	event := h.server.auditEvent(r, "authorization.membership.create", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	item, err := h.server.Store.PutWorkspaceMembership(r.Context(), workspace.ID, user.PublicID, input.Role, input.Status, nil, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *generatedHandler) PutWorkspaceMember(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, userID generated.UserId, params generated.PutWorkspaceMemberParams) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageMembers)
	if !ok {
		return
	}
	var input generated.WorkspaceMembershipInput
	if decodeJSON(r, &input) != nil || !validMembership(string(input.Role), string(input.Status)) {
		writeError(w, http.StatusBadRequest, "membership_invalid", "membership is invalid", r)
		return
	}
	var version *int64
	if params.IfMatch != nil {
		value := int64(*params.IfMatch)
		version = &value
	}
	event := h.server.auditEvent(r, "authorization.membership.put", "User", string(userID), audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	item, err := h.server.Store.PutWorkspaceMembership(r.Context(), workspace.ID, string(userID), string(input.Role), string(input.Status), version, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *generatedHandler) DeleteWorkspaceMember(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, userID generated.UserId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageMembers)
	if !ok {
		return
	}
	event := h.server.auditEvent(r, "authorization.membership.delete", "User", string(userID), audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	if err := h.server.Store.DeleteWorkspaceMembership(r.Context(), workspace.ID, string(userID), event); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) ListWorkspaceGroups(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	items, err := h.server.Store.ListWorkspaceGroups(r.Context(), workspace.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "groups could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) CreateWorkspaceGroup(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageGroups)
	if !ok {
		return
	}
	var input generated.WorkspaceGroupInput
	if decodeJSON(r, &input) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	name, nameKey, err := domain.NormalizeHierarchyName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "group_invalid", err.Error(), r)
		return
	}
	publicID, err := domain.NewPublicID("grp")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "group_create_failed", "group could not be created", r)
		return
	}
	event := h.server.auditEvent(r, "authorization.group.create", "Group", publicID, audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	group, err := h.server.Store.CreateWorkspaceGroup(r.Context(), store.Group{PublicID: publicID, WorkspaceID: workspace.ID, Name: name}, nameKey, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func (h *generatedHandler) ListWorkspaceGroupMembers(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, groupID generated.GroupId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	items, err := h.server.Store.ListWorkspaceGroupMembers(r.Context(), workspace.ID, string(groupID))
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) AddWorkspaceGroupMember(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, groupID generated.GroupId, userID generated.UserId) {
	h.mutateGroupMember(w, r, workspaceID, groupID, userID, true)
}

func (h *generatedHandler) RemoveWorkspaceGroupMember(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, groupID generated.GroupId, userID generated.UserId) {
	h.mutateGroupMember(w, r, workspaceID, groupID, userID, false)
}

func (h *generatedHandler) mutateGroupMember(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, groupID generated.GroupId, userID generated.UserId, add bool) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageGroups)
	if !ok {
		return
	}
	action := "authorization.group.member.remove"
	if add {
		action = "authorization.group.member.add"
	}
	event := h.server.auditEvent(r, action, "Group", string(groupID), audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	event.Metadata = map[string]any{"userId": string(userID)}
	var err error
	if add {
		err = h.server.Store.AddWorkspaceGroupMember(r.Context(), workspace.ID, string(groupID), string(userID), event)
	} else {
		err = h.server.Store.RemoveWorkspaceGroupMember(r.Context(), workspace.ID, string(groupID), string(userID), event)
	}
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) ListAuditEvents(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, params generated.ListAuditEventsParams) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadAudit)
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, next, err := h.server.Store.ListAuditEvents(r.Context(), workspace.ID, beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "audit events could not be listed", r)
		return
	}
	writeHierarchyList(w, items, next)
}

func (h *generatedHandler) ListWorkspaceAccessGrants(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	items, err := h.server.Store.ListWorkspaceAccessGrants(r.Context(), workspace.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "access grants could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) CreateWorkspaceAccessGrant(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageGroups)
	if !ok {
		return
	}
	var input struct {
		SubjectType  string `json:"subjectType"`
		SubjectID    string `json:"subjectId"`
		ResourceType string `json:"resourceType"`
		ResourceID   string `json:"resourceId"`
		Relation     string `json:"relation"`
	}
	if decodeJSON(r, &input) != nil || !validAccessGrant(input.SubjectType, input.ResourceType, input.Relation) {
		writeError(w, http.StatusBadRequest, "access_grant_invalid", "access grant is invalid", r)
		return
	}
	publicID, err := domain.NewPublicID("agr")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "access_grant_failed", "access grant could not be created", r)
		return
	}
	event := h.server.auditEvent(r, "authorization.access_grant.create", "AccessGrant", publicID, audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	grant, err := h.server.Store.CreateWorkspaceAccessGrant(r.Context(), store.AccessGrant{PublicID: publicID, WorkspaceID: workspace.ID, SubjectType: input.SubjectType, SubjectPublicID: input.SubjectID, ResourceType: input.ResourceType, ResourcePublicID: input.ResourceID, Relation: input.Relation}, actor.ID, event)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, grant)
}

func (h *generatedHandler) DeleteWorkspaceAccessGrant(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, accessGrantID generated.AccessGrantId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageGroups)
	if !ok {
		return
	}
	event := h.server.auditEvent(r, "authorization.access_grant.delete", "AccessGrant", string(accessGrantID), audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	if err := h.server.Store.DeleteWorkspaceAccessGrant(r.Context(), workspace.ID, string(accessGrantID), event); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) authorizeInstallation(w http.ResponseWriter, r *http.Request, mutation bool, permission authorization.Permission) (identity.User, bool) {
	user, ok := h.authorizeUser(w, r, mutation)
	if !ok {
		return identity.User{}, false
	}
	context, err := h.server.Store.AuthorizationContext(r.Context(), user.ID, 0, "Installation", "default")
	if err != nil || !authorization.Allowed(context, permission) {
		writeError(w, http.StatusForbidden, "permission_denied", "installation administration is required", r)
		return identity.User{}, false
	}
	return user, true
}

func (h *generatedHandler) authorizeWorkspacePermission(w http.ResponseWriter, r *http.Request, publicID string, mutation bool, permission authorization.Permission) (identity.User, domain.Workspace, bool) {
	user, ok := h.authorizeUser(w, r, mutation)
	if !ok {
		return identity.User{}, domain.Workspace{}, false
	}
	workspace, err := h.server.Store.FindWorkspaceForUser(r.Context(), user.ID, publicID)
	if err != nil {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
		return identity.User{}, domain.Workspace{}, false
	}
	context, err := h.server.Store.AuthorizationContext(r.Context(), user.ID, workspace.ID, "Workspace", workspace.PublicID)
	if err != nil || !authorization.Allowed(context, permission) {
		writeError(w, http.StatusForbidden, "permission_denied", "permission is required", r)
		return identity.User{}, domain.Workspace{}, false
	}
	return user, workspace, true
}

func validMembership(role, status string) bool {
	validRole := role == authorization.RoleOwner || role == authorization.RoleMember || role == authorization.RoleViewer
	return validRole && (status == "Active" || status == "Suspended")
}

func validAccessGrant(subjectType, resourceType, relation string) bool {
	validSubject := subjectType == "User" || subjectType == "Group"
	validResource := resourceType == "Workspace" || resourceType == "Project" || resourceType == "App" || resourceType == "AppEnvironment"
	validRelation := relation == string(authorization.RelationViewer) || relation == string(authorization.RelationEditor) || relation == string(authorization.RelationDeployer) || relation == string(authorization.RelationManager)
	return validSubject && validResource && validRelation
}

func writeIdentityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "resource_conflict", "resource changed or already exists", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "request could not be completed", r)
	}
}
