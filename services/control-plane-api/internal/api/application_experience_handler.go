package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

type appEnvironmentSetupInput struct {
	App struct {
		Mode string  `json:"mode"`
		ID   *string `json:"id"`
		Name *string `json:"name"`
	} `json:"app"`
	EnvironmentID string                `json:"environmentId"`
	ClusterID     string                `json:"clusterId"`
	Branch        string                `json:"branch"`
	WorkloadKind  domain.WorkloadKind   `json:"workloadKind"`
	Volume        *domain.VolumeRequest `json:"volume"`
	Configuration domain.RuntimeConfig  `json:"configuration"`
}

func (h *generatedHandler) GetWorkspaceSummary(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	summary, err := h.server.store.WorkspaceSummary(r.Context(), workspace.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "Workspace summary could not be loaded", r)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *generatedHandler) ListWorkspaceOperations(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, params generated.ListWorkspaceOperationsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	status, kind, appEnvironmentID := "", "", ""
	if params.Status != nil {
		status = string(*params.Status)
	}
	if params.Kind != nil {
		kind = string(*params.Kind)
	}
	if params.AppEnvironmentId != nil {
		appEnvironmentID = *params.AppEnvironmentId
	}
	items, nextCursor, err := h.server.store.ListWorkspaceOperations(r.Context(), workspace.ID, beforeID, limit, status, kind, appEnvironmentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "Workspace operations could not be loaded", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateProjectAppEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, _ generated.CreateProjectAppEnvironmentParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	var input appEnvironmentSetupInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	command, violations := h.appEnvironmentSetupCommand(workspace.ID, actor.ID, string(projectID), input)
	if len(violations) != 0 {
		writeDetailedError(w, http.StatusBadRequest, "validation_failed", "request contains invalid fields", violations, r)
		return
	}
	for _, endpoint := range command.Configuration.PublicEndpoints {
		if endpoint.Type == domain.EndpointTCP && !h.server.config.PublicTCPEnabled {
			writeError(w, http.StatusConflict, "public_tcp_unavailable", "public TCP endpoints are unavailable in this installation", r)
			return
		}
	}
	command.IdempotencyHash = auth.HashToken(idempotencyKey)
	command.RequestPayloadHash = scopedRequestPayloadHash(r, payloadHash)
	for range 3 {
		var err error
		command.AppEnvironmentID, err = domain.NewPublicID("aev")
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate application identifiers", r)
			return
		}
		if command.CreateApp {
			command.NewAppPublicID, err = domain.NewPublicID("app")
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate application identifiers", r)
				return
			}
		}
		app, target, _, err := h.server.store.CreateAppEnvironmentSetup(r.Context(), command)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeAppEnvironmentSetupError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"app": app, "appEnvironment": target})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate application identifiers", r)
}

func (h *generatedHandler) appEnvironmentSetupCommand(workspaceID, actorID int64, projectID string, input appEnvironmentSetupInput) (store.AppEnvironmentSetupCommand, []errorViolation) {
	command := store.AppEnvironmentSetupCommand{
		WorkspaceID: workspaceID, ActorID: actorID, ProjectPublicID: projectID, EnvironmentID: input.EnvironmentID,
		ClusterID: input.ClusterID, Branch: input.Branch, WorkloadKind: input.WorkloadKind,
		Configuration: domain.NormalizeRuntimeConfig(input.Configuration), Volume: input.Volume,
	}
	violations := make([]errorViolation, 0)
	switch input.App.Mode {
	case "New":
		command.CreateApp = true
		if input.App.ID != nil {
			violations = append(violations, errorViolation{Field: "/app/id", Code: "invalid_combination", Message: "id cannot be provided for a new App"})
		}
		if input.App.Name == nil {
			violations = append(violations, errorViolation{Field: "/app/name", Code: "required", Message: "name is required for a new App"})
		} else {
			name, key, err := domain.NormalizeHierarchyName(*input.App.Name)
			if err != nil {
				violations = append(violations, errorViolation{Field: "/app/name", Code: "invalid_name", Message: err.Error()})
			} else {
				command.AppName, command.AppNameKey = name, key
			}
		}
	case "Existing":
		if input.App.Name != nil {
			violations = append(violations, errorViolation{Field: "/app/name", Code: "invalid_combination", Message: "name cannot be provided for an existing App"})
		}
		if input.App.ID == nil || strings.TrimSpace(*input.App.ID) == "" {
			violations = append(violations, errorViolation{Field: "/app/id", Code: "required", Message: "id is required for an existing App"})
		} else {
			command.AppPublicID = strings.TrimSpace(*input.App.ID)
		}
	default:
		violations = append(violations, errorViolation{Field: "/app/mode", Code: "invalid_choice", Message: "mode must be Existing or New"})
	}
	if input.ClusterID == "" {
		violations = append(violations, errorViolation{Field: "/clusterId", Code: "required", Message: "clusterId is required"})
	}
	if err := domain.ValidateEnvironmentID(input.EnvironmentID); err != nil {
		violations = append(violations, errorViolation{Field: "/environmentId", Code: "invalid_environment", Message: err.Error()})
	}
	if input.Branch != "" {
		branch, err := domain.NormalizeSourceBranch(input.Branch)
		if err != nil {
			violations = append(violations, errorViolation{Field: "/branch", Code: "invalid_branch", Message: err.Error()})
		} else {
			command.Branch = branch
		}
	}
	if err := domain.ValidateWorkloadConfiguration(command.WorkloadKind, command.Configuration, command.Volume); err != nil {
		violations = append(violations, errorViolation{Field: "/workloadKind", Code: "invalid_workload", Message: err.Error()})
	}
	if err := domain.ValidateRuntimeConfig(command.Configuration, h.server.config.MaxReplicas, h.server.config.MaxCPU, h.server.config.MaxMemory); err != nil {
		var association domain.PublicationAssociationError
		if errors.As(err, &association) {
			violations = append(violations, errorViolation{Field: publicationAssociationField(association), Code: "publication_invalid", Message: "review this publication address"})
		} else {
			violations = append(violations, errorViolation{Field: "/configuration", Code: "invalid_configuration", Message: err.Error()})
		}
	}
	return command, violations
}

func writeAppEnvironmentSetupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNameConflict):
		writeDetailedError(w, http.StatusConflict, "name_conflict", "an active resource already uses this name", []errorViolation{{Field: "/app/name", Code: "name_conflict", Message: "an active App already uses this name"}}, r)
	case errors.Is(err, store.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with a different request", r)
	default:
		writeAppEnvironmentError(w, r, err)
	}
}
