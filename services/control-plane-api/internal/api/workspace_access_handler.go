package api

import (
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListWorkspaceMembers(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	items, err := h.server.store.ListWorkspaceMemberships(r.Context(), workspace.ID)
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
	user, err := h.server.store.FindUserByUsername(r.Context(), username)
	if err != nil {
		writeIdentityError(w, r, err)
		return
	}
	event := h.server.auditEvent(r, "authorization.membership.create", "User", user.PublicID, audit.Succeeded)
	event.ActorUserID = &actor.ID
	event.WorkspaceID = &workspace.ID
	item, err := h.server.store.PutWorkspaceMembership(r.Context(), workspace.ID, user.PublicID, input.Role, input.Status, nil, event)
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
	item, err := h.server.store.PutWorkspaceMembership(r.Context(), workspace.ID, string(userID), string(input.Role), string(input.Status), version, event)
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
	if err := h.server.store.DeleteWorkspaceMembership(r.Context(), workspace.ID, string(userID), event); err != nil {
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
	items, err := h.server.store.ListWorkspaceGroups(r.Context(), workspace.ID)
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
	group, err := h.server.store.CreateWorkspaceGroup(r.Context(), store.Group{PublicID: publicID, WorkspaceID: workspace.ID, Name: name}, nameKey, event)
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
	items, err := h.server.store.ListWorkspaceGroupMembers(r.Context(), workspace.ID, string(groupID))
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
		err = h.server.store.AddWorkspaceGroupMember(r.Context(), workspace.ID, string(groupID), string(userID), event)
	} else {
		err = h.server.store.RemoveWorkspaceGroupMember(r.Context(), workspace.ID, string(groupID), string(userID), event)
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
	items, next, err := h.server.store.ListAuditEvents(r.Context(), workspace.ID, beforeID, limit)
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
	items, err := h.server.store.ListWorkspaceAccessGrants(r.Context(), workspace.ID)
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
	grant, err := h.server.store.CreateWorkspaceAccessGrant(r.Context(), store.AccessGrant{PublicID: publicID, WorkspaceID: workspace.ID, SubjectType: input.SubjectType, SubjectPublicID: input.SubjectID, ResourceType: input.ResourceType, ResourcePublicID: input.ResourceID, Relation: input.Relation}, actor.ID, event)
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
	if err := h.server.store.DeleteWorkspaceAccessGrant(r.Context(), workspace.ID, string(accessGrantID), event); err != nil {
		writeIdentityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
