package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api/generated"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/parameters"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

type parameterInput struct {
	Path        string `json:"path"`
	Kind        string `json:"type"`
	Description string `json:"description"`
	Value       string `json:"value"`
}

func (h *generatedHandler) ListParameters(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, params generated.ListParametersParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.Store.ListParameters(r.Context(), workspace.ID, beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list Parameters", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateParameter(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	input, ok := decodeParameterInput(w, r)
	if !ok {
		return
	}
	for range 3 {
		publicID, err := h.server.parameterID()
		if err != nil {
			break
		}
		value, ok := h.parameterValue(w, r, workspace.PublicID, publicID, input, 0)
		if !ok {
			return
		}
		item, err := h.server.Store.CreateParameter(r.Context(), workspace.ID, actor.ID, publicID, input.Path, input.Kind, input.Description, value)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeParameterError(w, r, err)
			return
		}
		h.server.logger().Info("parameter created", "request_id", requestID(r), "workspace_id", workspace.PublicID, "parameter_id", item.PublicID, "parameter_type", item.Kind, "parameter_version", item.CurrentVersion)
		writeJSON(w, http.StatusCreated, item)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a Parameter identifier", r)
}

func (h *generatedHandler) GetParameter(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, parameterID generated.ParameterId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	item, err := h.server.Store.FindParameter(r.Context(), workspace.ID, string(parameterID))
	if err != nil {
		writeParameterError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *generatedHandler) ReplaceParameter(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, parameterID generated.ParameterId, params generated.ReplaceParameterParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	current, err := h.server.Store.FindParameter(r.Context(), workspace.ID, string(parameterID))
	if err != nil {
		writeParameterError(w, r, err)
		return
	}
	if current.Version != int64(params.IfMatch) {
		writeParameterError(w, r, store.ErrVersionConflict)
		return
	}
	input, ok := decodeParameterInput(w, r)
	if !ok {
		return
	}
	if input.Kind != current.Kind {
		writeError(w, http.StatusBadRequest, "parameter_type_immutable", "Parameter type cannot be changed", r)
		return
	}
	expectedSecretVersion := int64(0)
	if current.Kind == domain.ParameterSecret {
		_, expectedSecretVersion, err = h.server.Store.ParameterSecretReference(r.Context(), workspace.ID, current.PublicID)
		if err != nil {
			writeParameterError(w, r, err)
			return
		}
	}
	value, ok := h.parameterValue(w, r, workspace.PublicID, current.PublicID, input, expectedSecretVersion)
	if !ok {
		return
	}
	item, err := h.server.Store.ReplaceParameter(r.Context(), workspace.ID, actor.ID, current.PublicID, input.Path, input.Description, int64(params.IfMatch), value)
	if err != nil {
		writeParameterError(w, r, err)
		return
	}
	h.server.logger().Info("parameter replaced", "request_id", requestID(r), "workspace_id", workspace.PublicID, "parameter_id", item.PublicID, "parameter_type", item.Kind, "parameter_version", item.CurrentVersion)
	writeJSON(w, http.StatusOK, item)
}

func (h *generatedHandler) ArchiveParameter(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, parameterID generated.ParameterId, params generated.ArchiveParameterParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	item, err := h.server.Store.FindParameter(r.Context(), workspace.ID, string(parameterID))
	if err != nil {
		writeParameterError(w, r, err)
		return
	}
	if err = h.server.Store.ArchiveParameter(r.Context(), workspace.ID, item.PublicID, int64(params.IfMatch)); err != nil {
		writeParameterError(w, r, err)
		return
	}
	h.server.logger().Info("parameter archived", "request_id", requestID(r), "workspace_id", workspace.PublicID, "parameter_id", item.PublicID, "parameter_type", item.Kind)
	w.WriteHeader(http.StatusNoContent)
}

func decodeParameterInput(w http.ResponseWriter, r *http.Request) (parameterInput, bool) {
	var input parameterInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return parameterInput{}, false
	}
	path, err := domain.NormalizeParameterPath(input.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parameter_invalid", err.Error(), r)
		return parameterInput{}, false
	}
	input.Path = path
	input.Description = strings.TrimSpace(input.Description)
	if err = domain.ValidateParameter(input.Kind, input.Description, input.Value); err != nil {
		writeError(w, http.StatusBadRequest, "parameter_invalid", err.Error(), r)
		return parameterInput{}, false
	}
	return input, true
}

func (h *generatedHandler) parameterValue(w http.ResponseWriter, r *http.Request, workspaceID, parameterID string, input parameterInput, expectedVersion int64) (domain.ParameterValue, bool) {
	if input.Kind == domain.ParameterPlainText {
		return domain.ParameterValue{PlainTextValue: &input.Value}, true
	}
	if h.server.ParameterSecrets == nil || len(h.server.SecretFingerprintKey) < 32 {
		writeError(w, http.StatusServiceUnavailable, "secret_store_unavailable", "secret storage is not configured", r)
		return domain.ParameterValue{}, false
	}
	reference := secretReference(workspaceID, parameterID)
	backendVersion, err := h.server.ParameterSecrets.Put(r.Context(), reference, input.Value, expectedVersion)
	if errors.Is(err, parameters.ErrConflict) {
		writeError(w, http.StatusConflict, "secret_version_conflict", "secret changed since it was read", r)
		return domain.ParameterValue{}, false
	}
	if err != nil || backendVersion < 1 {
		writeError(w, http.StatusServiceUnavailable, "secret_store_unavailable", "secret storage is unavailable", r)
		return domain.ParameterValue{}, false
	}
	mac := hmac.New(sha256.New, h.server.SecretFingerprintKey)
	_, _ = mac.Write([]byte(input.Value))
	return domain.ParameterValue{SecretReference: reference, SecretBackendVersion: backendVersion, Fingerprint: mac.Sum(nil)}, true
}

func secretReference(workspaceID, parameterID string) string {
	return "workspaces/" + workspaceID + "/parameters/" + parameterID
}

func writeParameterError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "parameter_not_found", "Parameter was not found", r)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "Parameter changed since it was read", r)
	case errors.Is(err, store.ErrNameConflict), errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "parameter_conflict", "an active Parameter already uses this path", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "Parameter state could not be persisted", r)
	}
}
