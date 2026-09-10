package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) GetOperation(w http.ResponseWriter, r *http.Request, operationID string) {
	if usesAutomationAuthentication(r) {
		actor, ok := h.authenticateAutomation(w, r)
		if !ok {
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
