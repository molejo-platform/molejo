package api

import (
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
)

func (h *generatedHandler) Login(w http.ResponseWriter, r *http.Request, _ generated.LoginParams) {
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

func (h *generatedHandler) Logout(w http.ResponseWriter, r *http.Request, _ generated.LogoutParams) {
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
