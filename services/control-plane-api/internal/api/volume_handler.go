package api

import (
	"errors"
	"net/http"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api/generated"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/audit"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

func (h *generatedHandler) ListStorageProfiles(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	items, err := h.server.Store.ListStorageProfiles(r.Context(), workspace.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "storage profiles could not be listed", r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *generatedHandler) GetAppEnvironmentVolume(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	if _, ok = h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID)); !ok {
		return
	}
	volume, err := h.server.Store.FindAppVolume(r.Context(), workspace.ID, string(appEnvironmentID))
	if err != nil {
		writeVolumeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, volume)
}

func (h *generatedHandler) ExpandAppEnvironmentVolume(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.ExpandAppEnvironmentVolumeParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	if _, ok = h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID)); !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	var input generated.AppVolumeExpansionInput
	if err := decodeJSON(r, &input); err != nil || input.SizeGiB < 1 {
		writeError(w, http.StatusBadRequest, "volume_size_invalid", "sizeGiB must be positive", r)
		return
	}
	volume, operation, _, err := h.server.Store.ExpandAppVolume(r.Context(), workspace.ID, actor.ID, string(appEnvironmentID), int64(input.SizeGiB), int64(params.IfMatch), auth.HashToken(idempotencyKey), scopedBuildPayloadHash(r, payloadHash))
	if err != nil {
		writeVolumeError(w, r, err)
		return
	}
	h.server.logAcceptedOperation(r, operation)
	_ = h.server.recordAudit(r, audit.Event{ActorUserID: &actor.ID, WorkspaceID: &workspace.ID, Action: "storage.volume.expand", TargetType: "AppVolume", TargetPublicID: volume.PublicID, Outcome: audit.Succeeded})
	writeJSON(w, http.StatusAccepted, map[string]any{"volume": volume, "operation": operation})
}

func (h *generatedHandler) DeleteAppEnvironmentVolume(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.DeleteAppEnvironmentVolumeParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	if _, ok = h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID)); !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	volume, operation, _, err := h.server.Store.DeleteAppVolume(r.Context(), workspace.ID, actor.ID, string(appEnvironmentID), int64(params.IfMatch), auth.HashToken(idempotencyKey), scopedBuildPayloadHash(r, payloadHash))
	if err != nil {
		writeVolumeError(w, r, err)
		return
	}
	h.server.logAcceptedOperation(r, operation)
	_ = h.server.recordAudit(r, audit.Event{ActorUserID: &actor.ID, WorkspaceID: &workspace.ID, Action: "storage.volume.delete", TargetType: "AppVolume", TargetPublicID: volume.PublicID, Outcome: audit.Succeeded})
	writeJSON(w, http.StatusAccepted, map[string]any{"volume": volume, "operation": operation})
}

func writeVolumeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "volume_not_found", "persistent storage was not found", r)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "persistent storage changed since it was read", r)
	case errors.Is(err, store.ErrStorageProfileUnavailable):
		writeError(w, http.StatusConflict, "storage_profile_unavailable", "the selected storage profile is unavailable", r)
	case errors.Is(err, store.ErrStorageQuotaExceeded):
		writeError(w, http.StatusConflict, "storage_quota_exceeded", "the requested storage capacity is unavailable", r)
	case errors.Is(err, store.ErrVolumeAttached):
		writeError(w, http.StatusConflict, "volume_attached", "persistent storage must be detached before deletion", r)
	case errors.Is(err, store.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was reused with another request", r)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "volume_conflict", "persistent storage conflicts with the current state", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "persistent storage could not be changed", r)
	}
}
