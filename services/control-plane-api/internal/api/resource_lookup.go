package api

import "net/http"

func (h *generatedHandler) appExists(w http.ResponseWriter, r *http.Request, workspaceID int64, projectID, appID string) bool {
	_, err := h.server.store.FindApp(r.Context(), workspaceID, projectID, appID)
	if err != nil {
		writeBuildError(w, r, err)
		return false
	}
	return true
}
