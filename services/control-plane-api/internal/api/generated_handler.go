package api

import (
	"errors"
	"net/http"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api/generated"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
	"github.com/go-chi/chi/v5"
)

type generatedHandler struct {
	server *Server
}

var _ generated.ServerInterface = (*generatedHandler)(nil)

func (s *Server) generatedHandler() http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.SchemaReady(r.Context()); err != nil {
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
	actorID, _, ok := h.server.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return
	}
	h.server.sessionInfo(w, r, actorID)
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

func (h *generatedHandler) ListDeployments(w http.ResponseWriter, r *http.Request, _ generated.ListDeploymentsParams) {
	_, workspace, ok := h.authorize(w, r, false)
	if ok {
		h.server.listDeployments(w, r, workspace)
	}
}

func (h *generatedHandler) CreateDeployment(w http.ResponseWriter, r *http.Request, _ generated.CreateDeploymentParams) {
	actorID, workspace, ok := h.authorize(w, r, true)
	if ok {
		h.server.createDeployment(w, r, workspace, actorID)
	}
}

func (h *generatedHandler) GetDeployment(w http.ResponseWriter, r *http.Request, deploymentID string) {
	_, workspace, ok := h.authorize(w, r, false)
	if !ok {
		return
	}
	deployment, ok := h.deployment(w, r, workspace.ID, deploymentID)
	if ok {
		h.server.detailDeployment(w, r, workspace, deployment)
	}
}

func (h *generatedHandler) UpdateDeployment(w http.ResponseWriter, r *http.Request, deploymentID string, _ generated.UpdateDeploymentParams) {
	actorID, workspace, ok := h.authorize(w, r, true)
	if !ok {
		return
	}
	deployment, ok := h.deployment(w, r, workspace.ID, deploymentID)
	if ok {
		h.server.updateDeployment(w, r, workspace, actorID, deployment)
	}
}

func (h *generatedHandler) DeleteDeployment(w http.ResponseWriter, r *http.Request, deploymentID string, _ generated.DeleteDeploymentParams) {
	actorID, workspace, ok := h.authorize(w, r, true)
	if !ok {
		return
	}
	deployment, ok := h.deployment(w, r, workspace.ID, deploymentID)
	if ok {
		h.server.deleteDeployment(w, r, workspace, actorID, deployment)
	}
}

func (h *generatedHandler) ListDeploymentOperations(w http.ResponseWriter, r *http.Request, deploymentID string) {
	_, workspace, ok := h.authorize(w, r, false)
	if !ok {
		return
	}
	deployment, ok := h.deployment(w, r, workspace.ID, deploymentID)
	if !ok {
		return
	}
	operations, err := h.server.Store.ListOperations(r.Context(), workspace.ID, deployment.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not read operations", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": operations})
}

func (h *generatedHandler) GetOperation(w http.ResponseWriter, r *http.Request, operationID string) {
	_, workspace, ok := h.authorize(w, r, false)
	if !ok {
		return
	}
	operation, err := h.server.Store.GetOperation(r.Context(), workspace.ID, operationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "operation_not_found", "operation was not found", r)
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (h *generatedHandler) authorize(w http.ResponseWriter, r *http.Request, mutation bool) (int64, domain.Workspace, bool) {
	actorID, csrf, ok := h.server.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required", r)
		return 0, domain.Workspace{}, false
	}
	if mutation && !h.server.validCSRF(r, csrf) {
		writeError(w, http.StatusForbidden, "csrf_failed", "request could not be verified", r)
		return 0, domain.Workspace{}, false
	}
	workspace, err := h.server.Store.WorkspaceForActor(r.Context(), actorID)
	if err != nil {
		writeError(w, http.StatusForbidden, "workspace_forbidden", "workspace access is not configured", r)
		return 0, domain.Workspace{}, false
	}
	return actorID, workspace, true
}

func (h *generatedHandler) deployment(w http.ResponseWriter, r *http.Request, workspaceID int64, publicID string) (domain.Deployment, bool) {
	deployment, err := h.server.Store.FindDeployment(r.Context(), workspaceID, publicID)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNoRows) {
		writeError(w, http.StatusNotFound, "deployment_not_found", "deployment was not found", r)
		return domain.Deployment{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not read deployment", r)
		return domain.Deployment{}, false
	}
	return deployment, true
}
