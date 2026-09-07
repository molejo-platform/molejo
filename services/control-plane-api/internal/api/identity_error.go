package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func writeIdentityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
	case errors.Is(err, store.ErrGovernanceInvariant):
		writeError(w, http.StatusConflict, "governance_invariant", "the final active administrator or Workspace Owner cannot be removed", r)
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "resource_conflict", "resource changed or already exists", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "request could not be completed", r)
	}
}
