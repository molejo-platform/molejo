package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

const (
	githubInstallationStep  = "installation"
	githubAuthorizationStep = "authorization"
)

func (h *generatedHandler) ConnectGitHubInstallation(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	actor, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageWorkspace)
	if !ok {
		return
	}
	if !h.githubAvailable(w, r) {
		return
	}
	state, err := h.server.newToken(32)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "github_connection_failed", "could not begin GitHub connection", r)
		return
	}
	browser, err := h.server.newToken(32)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "github_connection_failed", "could not begin GitHub connection", r)
		return
	}
	if err = h.server.store.CreateGitHubConnectionState(r.Context(), auth.HashToken(state), auth.HashToken(browser), actor.ID, workspace.ID, githubInstallationStep, 0, time.Now().Add(h.server.config.GitHubStateTTL)); err != nil {
		h.server.logger().Error("create GitHub connection state", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_connection_failed", "could not begin GitHub connection", r)
		return
	}
	h.setGitHubCookie(w, r, browser)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": h.server.github.InstallationURL(state)})
}

func (h *generatedHandler) CompleteGitHubInstallation(w http.ResponseWriter, r *http.Request, params generated.CompleteGitHubInstallationParams) {
	if !h.githubAvailable(w, r) {
		return
	}
	browser, ok := h.githubBrowser(w, r)
	if !ok {
		return
	}
	pending, err := h.server.store.ConsumeGitHubConnectionState(r.Context(), auth.HashToken(params.State), auth.HashToken(browser), githubInstallationStep)
	if err != nil {
		writeError(w, http.StatusBadRequest, "github_state_invalid", "GitHub connection state is invalid or expired", r)
		return
	}
	authorized, err := h.server.store.GitHubConnectionAuthorized(r.Context(), pending.UserID, pending.WorkspaceID)
	if err != nil || !authorized {
		writeError(w, http.StatusForbidden, "github_connection_forbidden", "GitHub connection is no longer authorized", r)
		return
	}
	if _, err = h.server.github.Installation(r.Context(), params.InstallationId); err != nil {
		h.server.logger().Warn("verify GitHub installation", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusBadRequest, "github_installation_invalid", "GitHub installation could not be verified", r)
		return
	}
	state, err := h.server.newToken(32)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "github_connection_failed", "could not continue GitHub connection", r)
		return
	}
	if err = h.server.store.CreateGitHubConnectionState(r.Context(), auth.HashToken(state), auth.HashToken(browser), pending.UserID, pending.WorkspaceID, githubAuthorizationStep, params.InstallationId, time.Now().Add(h.server.config.GitHubStateTTL)); err != nil {
		h.server.logger().Error("create GitHub authorization state", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_connection_failed", "could not continue GitHub connection", r)
		return
	}
	http.Redirect(w, r, h.server.github.UserAuthorizationURL(state), http.StatusFound)
}

func (h *generatedHandler) CompleteGitHubAuthorization(w http.ResponseWriter, r *http.Request, params generated.CompleteGitHubAuthorizationParams) {
	if !h.githubAvailable(w, r) {
		return
	}
	browser, ok := h.githubBrowser(w, r)
	if !ok {
		return
	}
	pending, err := h.server.store.ConsumeGitHubConnectionState(r.Context(), auth.HashToken(params.State), auth.HashToken(browser), githubAuthorizationStep)
	if err != nil {
		writeError(w, http.StatusBadRequest, "github_state_invalid", "GitHub authorization state is invalid or expired", r)
		return
	}
	authorized, err := h.server.store.GitHubConnectionAuthorized(r.Context(), pending.UserID, pending.WorkspaceID)
	if err != nil || !authorized {
		writeError(w, http.StatusForbidden, "github_connection_forbidden", "GitHub connection is no longer authorized", r)
		return
	}
	allowed, err := h.server.github.UserCanAccessInstallation(r.Context(), params.Code, pending.InstallationID)
	if err != nil || !allowed {
		if err != nil {
			h.server.logger().Warn("verify GitHub user installation", "request_id", requestID(r), "error", err)
		}
		writeError(w, http.StatusForbidden, "github_installation_forbidden", "the authorized GitHub user cannot access this installation", r)
		return
	}
	installation, err := h.server.github.Installation(r.Context(), pending.InstallationID)
	if err != nil {
		h.server.logger().Warn("read GitHub installation", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_unavailable", "GitHub installation could not be read", r)
		return
	}
	for range 3 {
		publicID, idErr := domain.NewPublicID("ghi")
		if idErr != nil {
			break
		}
		_, err = h.server.store.ConnectGitHubInstallation(r.Context(), publicID, pending.WorkspaceID, pending.UserID, installation.ID, installation.AccountID, installation.AccountLogin, installation.AccountType, installation.RepositorySelection)
		if !errors.Is(err, store.ErrPublicIDCollision) {
			break
		}
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "github_installation_connected", "GitHub installation is already connected to another Workspace", r)
		return
	}
	if err != nil {
		h.server.logger().Error("persist GitHub installation", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_connection_failed", "GitHub installation could not be connected", r)
		return
	}
	h.clearGitHubCookie(w, r)
	workspace, err := h.server.store.Workspace(r.Context(), pending.WorkspaceID)
	if err != nil {
		h.server.logger().Error("read GitHub callback workspace", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_connection_failed", "GitHub connection return path could not be resolved", r)
		return
	}
	http.Redirect(w, r, h.server.config.PublicURL+"/workspaces/"+url.PathEscape(workspace.PublicID)+"/settings/github?github=connected", http.StatusFound)
}

func (h *generatedHandler) ListGitHubInstallations(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	items, err := h.server.store.ListGitHubInstallations(r.Context(), workspace.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list GitHub installations", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) ListGitHubRepositories(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, githubInstallationID generated.GitHubInstallationId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok || !h.githubAvailable(w, r) {
		return
	}
	installation, err := h.server.store.FindGitHubInstallation(r.Context(), workspace.ID, string(githubInstallationID))
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	items, err := h.server.github.Repositories(r.Context(), installation.ExternalID)
	if err != nil {
		h.server.logger().Warn("list GitHub repositories", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_unavailable", "GitHub repositories could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) DisconnectGitHubInstallation(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, githubInstallationID generated.GitHubInstallationId) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), true, authorization.ManageWorkspace)
	if !ok || !h.githubAvailable(w, r) {
		return
	}
	installation, err := h.server.store.FindGitHubInstallation(r.Context(), workspace.ID, string(githubInstallationID))
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	inUse, err := h.server.store.GitHubInstallationInUse(r.Context(), installation.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not inspect GitHub installation", r)
		return
	}
	if inUse {
		writeError(w, http.StatusConflict, "github_installation_in_use", "remove this installation from every App before disconnecting it", r)
		return
	}
	if err = h.server.github.DeleteInstallation(r.Context(), installation.ExternalID); err != nil {
		h.server.logger().Error("uninstall GitHub App", "request_id", requestID(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "github_uninstall_failed", "the GitHub App could not be uninstalled", r)
		return
	}
	if err = h.server.store.DeleteGitHubInstallation(r.Context(), workspace.ID, installation.PublicID); err != nil {
		h.server.logger().Error("remove GitHub installation", "request_id", requestID(r), "error", err)
		writeHierarchyError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) GetAppSource(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	source, err := h.server.store.GetAppGitHubSource(r.Context(), workspace.ID, string(projectID), string(appID))
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": source})
}

func (h *generatedHandler) SetAppSource(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok || !h.githubAvailable(w, r) {
		return
	}
	var input struct {
		InstallationID string `json:"installationId"`
		RepositoryID   string `json:"repositoryId"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	installation, err := h.server.store.FindGitHubInstallation(r.Context(), workspace.ID, input.InstallationID)
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	repositories, err := h.server.github.Repositories(r.Context(), installation.ExternalID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "github_unavailable", "GitHub repositories could not be listed", r)
		return
	}
	var selected *domain.GitHubRepository
	for index := range repositories {
		if repositories[index].ID == input.RepositoryID {
			selected = &repositories[index]
			break
		}
	}
	if selected == nil {
		writeError(w, http.StatusNotFound, "repository_not_found", "repository is not accessible to this installation", r)
		return
	}
	source, err := h.server.store.SetAppGitHubSource(r.Context(), workspace.ID, string(projectID), string(appID), installation.PublicID, *selected)
	if err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, source)
}

func (h *generatedHandler) ClearAppSource(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	if err := h.server.store.ClearAppGitHubSource(r.Context(), workspace.ID, string(projectID), string(appID)); err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *generatedHandler) githubAvailable(w http.ResponseWriter, r *http.Request) bool {
	if h.server.github != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "github_not_configured", "GitHub App integration is not configured", r)
	return false
}

func (h *generatedHandler) githubBrowser(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(h.server.config.GitHubCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusBadRequest, "github_state_invalid", "GitHub connection state is invalid or expired", r)
		return "", false
	}
	return cookie.Value, true
}

func (h *generatedHandler) setGitHubCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{Name: h.server.config.GitHubCookieName, Value: value, Path: "/api/v1/github", HttpOnly: true, Secure: h.server.config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: int(h.server.config.GitHubStateTTL.Seconds())})
}

func (h *generatedHandler) clearGitHubCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: h.server.config.GitHubCookieName, Value: "", Path: "/api/v1/github", HttpOnly: true, Secure: h.server.config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
