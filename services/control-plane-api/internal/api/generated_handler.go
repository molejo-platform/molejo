package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

type generatedHandler struct {
	server *Server
}

var _ generated.ServerInterface = (*generatedHandler)(nil)

func (s *Server) generatedHandler() http.Handler {
	router := chi.NewRouter()
	router.Use(tracingMiddleware(s))
	router.Use(func(next http.Handler) http.Handler { return securityMiddleware(s, next) })
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.SchemaReady(r.Context()); err != nil {
			s.logger().Error("schema readiness failed", "request_id", requestID(r), "error", err)
			writeError(w, http.StatusServiceUnavailable, "database_unavailable", "service is not ready", r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	return generated.HandlerWithOptions(&generatedHandler{server: s}, generated.ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			writeError(w, http.StatusBadRequest, "invalid_request", "request parameters are invalid", r)
		},
	})
}

func (h *generatedHandler) Login(w http.ResponseWriter, r *http.Request) {
	h.server.login(w, r)
}

func (h *generatedHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.server.sessionPrincipal(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	h.server.sessionInfo(w, r, principal.UserID, principal.AssuranceLevel)
}

func (h *generatedHandler) Logout(w http.ResponseWriter, r *http.Request) {
	_, csrf, ok := h.server.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	if !h.server.validCSRF(r, csrf) {
		writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
		return
	}
	h.server.logout(w, r)
}

func (h *generatedHandler) GetCurrentWorkspace(w http.ResponseWriter, r *http.Request) {
	_, workspace, ok := h.authorize(w, r, false)
	if ok {
		writeJSON(w, http.StatusOK, workspace)
	}
}

func (h *generatedHandler) GetOperation(w http.ResponseWriter, r *http.Request, operationID string) {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		token, valid := automationBearerToken(r.Header.Get("Authorization"))
		if !valid {
			writeError(w, http.StatusUnauthorized, "automation_unauthenticated", "valid service account bearer token is required", r)
			return
		}
		actor, err := h.server.store.AuthenticateServiceAccount(r.Context(), auth.HashToken(token))
		if err != nil {
			writeAutomationError(w, r, err)
			return
		}
		operation, err := h.server.store.GetOperationForPrincipal(r.Context(), actor.ID, operationID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "operation_not_found", "operation was not found", r)
			} else {
				writeError(w, http.StatusInternalServerError, "storage_failed", "operation could not be loaded", r)
			}
			return
		}
		writeJSON(w, http.StatusOK, operation)
		return
	}
	userID, _, ok := h.server.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	operation, err := h.server.store.GetOperationForUser(r.Context(), userID, operationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "operation_not_found", "operation was not found", r)
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

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
