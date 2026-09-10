package api

import (
	"net/http"
	"strings"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
)

func (h *generatedHandler) authorize(w http.ResponseWriter, r *http.Request, mutation bool) (int64, domain.Workspace, bool) {
	userID, csrf, ok := h.server.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return 0, domain.Workspace{}, false
	}
	if mutation && !h.server.validCSRF(r, csrf) {
		writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
		return 0, domain.Workspace{}, false
	}
	workspace, err := h.server.store.WorkspaceForUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusForbidden, "workspace_forbidden", "workspace access is not configured", r)
		return 0, domain.Workspace{}, false
	}
	permission := authorization.ReadWorkspace
	if mutation {
		permission = authorization.EditResources
	}
	context, err := h.server.store.AuthorizationContext(r.Context(), userID, workspace.ID, "Workspace", workspace.PublicID)
	if err != nil || !authorization.Allowed(context, permission) {
		writeError(w, http.StatusForbidden, "permission_denied", "permission is required", r)
		return 0, domain.Workspace{}, false
	}
	return userID, workspace, true
}

func (h *generatedHandler) authorizeUser(w http.ResponseWriter, r *http.Request, mutation bool) (identity.User, bool) {
	userID, csrf, ok := h.server.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return identity.User{}, false
	}
	user, err := h.server.store.User(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return identity.User{}, false
	}
	if mutation && !h.server.validCSRF(r, csrf) {
		writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
		return identity.User{}, false
	}
	return user, true
}

func (h *generatedHandler) authorizeWorkspace(w http.ResponseWriter, r *http.Request, publicID string, mutation bool) (identity.User, domain.Workspace, bool) {
	user, ok := h.authorizeUser(w, r, mutation)
	if !ok {
		return identity.User{}, domain.Workspace{}, false
	}
	workspace, err := h.server.store.FindWorkspaceForUser(r.Context(), user.ID, publicID)
	if err != nil {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
		return identity.User{}, domain.Workspace{}, false
	}
	permission := authorization.ReadWorkspace
	resourceType, resourceID := "Workspace", workspace.PublicID
	if mutation {
		permission = mutationPermission(r)
		resourceType, resourceID = authorizationResource(r.URL.Path, workspace.PublicID)
	}
	context, err := h.server.store.AuthorizationContext(r.Context(), user.ID, workspace.ID, resourceType, resourceID)
	if err != nil || !authorization.Allowed(context, permission) {
		writeError(w, http.StatusForbidden, "permission_denied", "permission is required", r)
		return identity.User{}, domain.Workspace{}, false
	}
	return user, workspace, true
}

func (h *generatedHandler) authorizeInstallation(w http.ResponseWriter, r *http.Request, mutation bool, permission authorization.Permission) (identity.User, bool) {
	user, ok := h.authorizeUser(w, r, mutation)
	if !ok {
		return identity.User{}, false
	}
	context, err := h.server.store.AuthorizationContext(r.Context(), user.ID, 0, "Installation", "default")
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
	workspace, err := h.server.store.FindWorkspaceForUser(r.Context(), user.ID, publicID)
	if err != nil {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
		return identity.User{}, domain.Workspace{}, false
	}
	context, err := h.server.store.AuthorizationContext(r.Context(), user.ID, workspace.ID, "Workspace", workspace.PublicID)
	if err != nil || !authorization.Allowed(context, permission) {
		writeError(w, http.StatusForbidden, "permission_denied", "permission is required", r)
		return identity.User{}, domain.Workspace{}, false
	}
	return user, workspace, true
}

func mutationPermission(r *http.Request) authorization.Permission {
	if r.Method == http.MethodPost && (strings.Contains(r.URL.Path, "/builds") || strings.Contains(r.URL.Path, "/deployments") || strings.Contains(r.URL.Path, "/releases")) {
		return authorization.Deploy
	}
	return authorization.EditResources
}

func authorizationResource(path, workspaceID string) (string, string) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	resourceType, resourceID := "Workspace", workspaceID
	for index := 0; index+1 < len(segments); index++ {
		switch segments[index] {
		case "projects":
			resourceType, resourceID = "Project", segments[index+1]
		case "apps":
			if resourceType == "Project" {
				resourceType, resourceID = "App", segments[index+1]
			}
		case "environments":
			if resourceType == "App" {
				candidate := segments[index+1]
				if strings.HasPrefix(candidate, "aev-") {
					resourceType, resourceID = "AppEnvironment", candidate
				}
			}
		}
	}
	return resourceType, resourceID
}
