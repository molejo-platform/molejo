package api

import "net/http"

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
