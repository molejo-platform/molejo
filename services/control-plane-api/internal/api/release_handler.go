package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	releasecontract "github.com/molejo-platform/molejo/services/control-plane-api/internal/release"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func (h *generatedHandler) RegisterAppRelease(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, _ generated.RegisterAppReleaseParams) {
	actor, workspace, ok := h.authorizeAutomation(w, r, string(workspaceID), string(projectID), string(appID), "", automation.PermissionReleaseWrite)
	if !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	var input generated.ReleaseRegistrationInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	command := releaseRegistrationCommand(input)
	if err := releasecontract.Validate(command); err != nil {
		writeError(w, http.StatusBadRequest, "release_invalid", err.Error(), r)
		return
	}
	if !h.server.config.RegistryAllowed(command.Artifact.Reference) {
		writeError(w, http.StatusBadRequest, "registry_not_allowed", "artifact registry is not allowed", r)
		return
	}
	for range 3 {
		releaseID, err := domain.NewPublicID("rel")
		if err != nil {
			break
		}
		event := h.server.auditEvent(r, "release.register", "Release", releaseID, audit.Succeeded)
		item, replay, err := h.server.store.RegisterExternalRelease(r.Context(), actor, store.RegisterExternalReleaseParams{
			WorkspaceID:     workspace.ID,
			ProjectPublicID: string(projectID),
			AppPublicID:     string(appID),
			ReleasePublicID: releaseID,
			Command:         command,
			IdempotencyHash: auth.HashToken(idempotencyKey),
			PayloadHash:     scopedRequestPayloadHash(r, payloadHash),
			AuditEvent:      event,
		})
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeAutomationError(w, r, err)
			return
		}
		status := http.StatusCreated
		if replay {
			status = http.StatusOK
		}
		writeJSON(w, status, item)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a Release identifier", r)
}

func (h *generatedHandler) ListAppReleases(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, params generated.ListAppReleasesParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.store.ListReleases(r.Context(), workspace.ID, string(projectID), string(appID), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list releases", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func releaseRegistrationCommand(input generated.ReleaseRegistrationInput) releasecontract.RegisterCommand {
	return releasecontract.RegisterCommand{
		Artifact: releasecontract.Artifact{
			Kind:      string(input.Artifact.Kind),
			Reference: input.Artifact.Reference,
		},
		Source: releasecontract.Source{
			Provider:   input.Source.Provider,
			Repository: input.Source.Repository,
			Revision:   input.Source.Revision,
			Ref:        optionalString(input.Source.Ref),
		},
		Provenance: releasecontract.Provenance{
			Producer:      input.Provenance.Producer,
			ExternalRunID: optionalString(input.Provenance.ExternalRunId),
			URL:           optionalString(input.Provenance.Url),
		},
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
