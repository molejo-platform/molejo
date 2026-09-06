package api

import "net/http"

func (h *generatedHandler) GetCurrentWorkspace(w http.ResponseWriter, r *http.Request) {
	_, workspace, ok := h.authorize(w, r, false)
	if ok {
		writeJSON(w, http.StatusOK, workspace)
	}
}
