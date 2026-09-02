package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/parameters"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
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
	items, nextCursor, err := h.server.store.ListParameters(r.Context(), workspace.ID, beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list Parameters", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateParameter(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, _ generated.CreateParameterParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	idempotencyKey, payloadHash, hasIdempotency := idempotency(r)
	input, ok := decodeParameterInput(w, r)
	if !ok {
		return
	}
	if input.Kind == domain.ParameterPlainText {
		for range 3 {
			publicID, err := h.server.parameterID()
			if err != nil {
				break
			}
			value := domain.ParameterValue{PlainTextValue: &input.Value}
			item, createErr := h.server.store.CreateParameter(r.Context(), workspace.ID, actor.ID, publicID, input.Path, input.Kind, input.Description, value)
			if errors.Is(createErr, store.ErrPublicIDCollision) {
				continue
			}
			if createErr != nil {
				writeParameterError(w, r, createErr)
				return
			}
			h.server.logger().Info("parameter created", "request_id", requestID(r), "workspace_id", workspace.PublicID, "parameter_id", item.PublicID, "parameter_type", item.Kind, "parameter_version", item.CurrentVersion)
			writeJSON(w, http.StatusCreated, item)
			return
		}
		writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a Parameter identifier", r)
		return
	}
	if !hasIdempotency {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required for Secret parameters", r)
		return
	}
	payloadHash = scopedBuildPayloadHash(r, payloadHash)
	if !h.secretBackendAvailable(w, r) {
		return
	}
	fingerprint := h.secretFingerprint(input.Value)
	for range 3 {
		publicID, err := h.server.parameterID()
		if err != nil {
			break
		}
		mutation, existing, beginErr := h.server.store.BeginCreateSecretParameter(r.Context(), workspace.ID, actor.ID, publicID, input.Path, input.Description, secretReference(workspace.PublicID, publicID), fingerprint, auth.HashToken(idempotencyKey), payloadHash)
		if errors.Is(beginErr, store.ErrPublicIDCollision) {
			continue
		}
		if beginErr != nil {
			writeParameterError(w, r, beginErr)
			return
		}
		if existing && mutation.State == "Ready" {
			item, findErr := h.server.store.FindParameter(r.Context(), workspace.ID, mutation.ParameterPublicID)
			if findErr != nil {
				writeParameterError(w, r, findErr)
				return
			}
			writeJSON(w, http.StatusCreated, item)
			return
		}
		item, ok := h.completeSecretMutation(w, r, mutation, input.Value)
		if !ok {
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
	item, err := h.server.store.FindParameter(r.Context(), workspace.ID, string(parameterID))
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
	idempotencyKey, payloadHash, hasIdempotency := idempotency(r)
	input, ok := decodeParameterInput(w, r)
	if !ok {
		return
	}
	current, err := h.server.store.FindParameter(r.Context(), workspace.ID, string(parameterID))
	if err != nil {
		writeParameterError(w, r, err)
		return
	}
	if input.Kind != current.Kind {
		writeError(w, http.StatusBadRequest, "parameter_type_immutable", "Parameter type cannot be changed", r)
		return
	}
	if current.Kind == domain.ParameterPlainText {
		if current.Version != int64(params.IfMatch) {
			writeParameterError(w, r, store.ErrVersionConflict)
			return
		}
		value := domain.ParameterValue{PlainTextValue: &input.Value}
		item, replaceErr := h.server.store.ReplaceParameter(r.Context(), workspace.ID, actor.ID, current.PublicID, input.Path, input.Description, int64(params.IfMatch), value)
		if replaceErr != nil {
			writeParameterError(w, r, replaceErr)
			return
		}
		h.server.logger().Info("parameter replaced", "request_id", requestID(r), "workspace_id", workspace.PublicID, "parameter_id", item.PublicID, "parameter_type", item.Kind, "parameter_version", item.CurrentVersion)
		writeJSON(w, http.StatusOK, item)
		return
	}
	if !hasIdempotency {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required for Secret parameters", r)
		return
	}
	payloadHash = scopedBuildPayloadHash(r, payloadHash)
	if !h.secretBackendAvailable(w, r) {
		return
	}
	mutation, existing, err := h.server.store.BeginReplaceSecretParameter(r.Context(), workspace.ID, actor.ID, current.PublicID, input.Path, input.Description, int64(params.IfMatch), h.secretFingerprint(input.Value), auth.HashToken(idempotencyKey), payloadHash)
	if err != nil {
		writeParameterError(w, r, err)
		return
	}
	if existing && mutation.State == "Ready" {
		item, findErr := h.server.store.FindParameter(r.Context(), workspace.ID, mutation.ParameterPublicID)
		if findErr != nil {
			writeParameterError(w, r, findErr)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	item, ok := h.completeSecretMutation(w, r, mutation, input.Value)
	if !ok {
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
	item, err := h.server.store.FindParameter(r.Context(), workspace.ID, string(parameterID))
	if err != nil {
		writeParameterError(w, r, err)
		return
	}
	if err = h.server.store.ArchiveParameter(r.Context(), workspace.ID, item.PublicID, int64(params.IfMatch), h.server.config.ParameterRetention); err != nil {
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

func (h *generatedHandler) secretBackendAvailable(w http.ResponseWriter, r *http.Request) bool {
	if h.server.parameterSecrets == nil || len(h.server.secretFingerprintKey) < 32 {
		writeError(w, http.StatusServiceUnavailable, "secret_store_unavailable", "secret storage is not configured", r)
		return false
	}
	return true
}

func (h *generatedHandler) secretFingerprint(value string) []byte {
	mac := hmac.New(sha256.New, h.server.secretFingerprintKey)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func (h *generatedHandler) completeSecretMutation(w http.ResponseWriter, r *http.Request, mutation domain.SecretMutation, value string) (domain.Parameter, bool) {
	backendVersion, err := h.server.parameterSecrets.Put(r.Context(), mutation.Reference, value, mutation.ExpectedBackendVersion)
	if errors.Is(err, parameters.ErrConflict) {
		currentVersion, inspectErr := h.server.parameterSecrets.CurrentVersion(r.Context(), mutation.Reference)
		if inspectErr == nil && currentVersion == mutation.BackendVersion {
			backendVersion = currentVersion
			err = nil
		} else {
			writeError(w, http.StatusConflict, "secret_version_conflict", "secret changed since the mutation was reserved", r)
			return domain.Parameter{}, false
		}
	}
	if err != nil || backendVersion < 1 {
		writeError(w, http.StatusServiceUnavailable, "secret_store_unavailable", "secret storage is unavailable", r)
		return domain.Parameter{}, false
	}
	item, err := h.server.store.CompleteSecretMutation(r.Context(), mutation, backendVersion)
	if err != nil {
		writeParameterError(w, r, err)
		return domain.Parameter{}, false
	}
	return item, true
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
	case errors.Is(err, store.ErrParameterInUse):
		writeError(w, http.StatusConflict, "parameter_in_use", "Parameter is still referenced by desired, running, or deployed configuration", r)
	case errors.Is(err, store.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used with another Parameter mutation", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "Parameter state could not be persisted", r)
	}
}
