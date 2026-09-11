package api

import (
	"errors"
	"net/http"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/automation"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/principal"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

type appEnvironmentInput struct {
	EnvironmentID string                `json:"environmentId"`
	ClusterID     string                `json:"clusterId"`
	Branch        string                `json:"branch"`
	WorkloadKind  domain.WorkloadKind   `json:"workloadKind"`
	Volume        *domain.VolumeRequest `json:"volume"`
	Configuration domain.RuntimeConfig  `json:"configuration"`
}

func (h *generatedHandler) ListAppEnvironments(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, params generated.ListAppEnvironmentsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok || !h.appExists(w, r, workspace.ID, string(projectID), string(appID)) {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.store.ListAppEnvironments(r.Context(), workspace.ID, string(projectID), string(appID), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list App Environments", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) ListEnvironmentApps(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, environmentID generated.EnvironmentId, params generated.ListEnvironmentAppsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	if _, err := h.server.store.FindEnvironment(r.Context(), workspace.ID, string(projectID), string(environmentID)); err != nil {
		writeHierarchyError(w, r, err)
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.store.ListEnvironmentApps(r.Context(), workspace.ID, string(projectID), string(environmentID), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list Environment Apps", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateAppEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	input, ok := h.appEnvironmentInput(w, r, true)
	if !ok {
		return
	}
	for range 3 {
		publicID, err := domain.NewPublicID("aev")
		if err != nil {
			break
		}
		item, _, err := h.server.store.CreateAppEnvironmentOnCluster(r.Context(), workspace.ID, actor.ID, publicID, string(projectID), string(appID), input.EnvironmentID, input.ClusterID, input.Branch, input.WorkloadKind, input.Configuration, input.Volume)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		if err != nil {
			writeAppEnvironmentError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate an App Environment identifier", r)
}

func (h *generatedHandler) GetAppEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) {
	var workspace domain.Workspace
	var ok bool
	if usesAutomationAuthentication(r) {
		_, workspace, ok = h.authorizeAutomation(w, r, string(workspaceID), string(projectID), string(appID), string(appEnvironmentID), automation.PermissionDeploymentCreate)
	} else {
		_, workspace, ok = h.authorizeWorkspace(w, r, string(workspaceID), false)
	}
	if !ok {
		return
	}
	item, ok := h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *generatedHandler) UpdateAppEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.UpdateAppEnvironmentParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	if _, ok = h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID)); !ok {
		return
	}
	input, ok := h.appEnvironmentInput(w, r, false)
	if !ok {
		return
	}
	item, err := h.server.store.UpdateAppEnvironment(r.Context(), workspace.ID, actor.ID, string(appEnvironmentID), input.Branch, input.Configuration, int64(params.IfMatch))
	if err != nil {
		writeAppEnvironmentError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *generatedHandler) DeleteAppEnvironment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.DeleteAppEnvironmentParams) {
	actor, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), true)
	if !ok {
		return
	}
	idempotencyKey, payloadHash, ok := idempotency(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required", r)
		return
	}
	idempotencyHash := auth.HashToken(idempotencyKey)
	requestHash := scopedRequestPayloadHash(r, payloadHash)
	operation, found, err := h.server.store.FindOperationByIdempotency(r.Context(), workspace.ID, actor.ID, idempotencyHash, requestHash)
	if err != nil {
		writeAppEnvironmentError(w, r, err)
		return
	}
	if found {
		h.server.logAcceptedOperation(r, operation)
		writeJSON(w, http.StatusAccepted, operation)
		return
	}
	if _, ok = h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID)); !ok {
		return
	}
	operation, err = h.server.store.DeleteAppEnvironment(r.Context(), workspace.ID, actor.ID, string(appEnvironmentID), int64(params.IfMatch), idempotencyHash, requestHash)
	if err != nil {
		writeAppEnvironmentError(w, r, err)
		return
	}
	h.server.logAcceptedOperation(r, operation)
	writeJSON(w, http.StatusAccepted, operation)
}

func (h *generatedHandler) ListAppEnvironmentDeployments(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.ListAppEnvironmentDeploymentsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	appEnvironment, ok := h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	if !ok {
		return
	}
	beforeID, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.store.ListDeployments(r.Context(), workspace.ID, appEnvironment.ID, beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list Deployments", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) CreateAppEnvironmentDeployment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.CreateAppEnvironmentDeploymentParams) {
	var actorUserID int64
	var automationActor principal.Principal
	var workspace domain.Workspace
	var ok bool
	isAutomation := usesAutomationAuthentication(r)
	if isAutomation {
		automationActor, workspace, ok = h.authorizeAutomation(w, r, string(workspaceID), string(projectID), string(appID), string(appEnvironmentID), automation.PermissionDeploymentCreate)
	} else {
		actor, authorizedWorkspace, authorized := h.authorizeWorkspace(w, r, string(workspaceID), true)
		if authorized {
			actorUserID, workspace, ok = actor.ID, authorizedWorkspace, true
		}
	}
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
	var input struct {
		ReleaseID            string  `json:"releaseId"`
		ConfigurationVersion int64   `json:"configurationVersion"`
		CurrentDeploymentID  *string `json:"currentDeploymentId"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	expectedCurrentDeploymentID := ""
	if input.CurrentDeploymentID != nil {
		expectedCurrentDeploymentID = *input.CurrentDeploymentID
	}
	for range 3 {
		deploymentID, err := h.server.deploymentID()
		if err != nil {
			break
		}
		var deployment domain.Deployment
		var operation domain.Operation
		var createErr error
		event := h.server.auditEvent(r, "deployment.create", "Deployment", deploymentID, audit.Succeeded)
		request := domain.DeploymentRequest{
			WorkspaceID:                       workspace.ID,
			AppEnvironmentPublicID:            string(appEnvironmentID),
			DeploymentPublicID:                deploymentID,
			ReleasePublicID:                   input.ReleaseID,
			ConfigurationVersion:              input.ConfigurationVersion,
			ExpectedVersion:                   int64(params.IfMatch),
			ExpectedCurrentDeploymentPublicID: expectedCurrentDeploymentID,
			IdempotencyHash:                   auth.HashToken(idempotencyKey),
			PayloadHash:                       scopedRequestPayloadHash(r, payloadHash),
		}
		if isAutomation {
			deployment, operation, _, createErr = h.server.store.CreateDeploymentForPrincipal(r.Context(), automationActor, request, event)
		} else {
			deployment, operation, _, createErr = h.server.store.CreateDeployment(r.Context(), actorUserID, request, event)
		}
		if errors.Is(createErr, store.ErrPublicIDCollision) {
			continue
		}
		if createErr != nil {
			writeAppEnvironmentError(w, r, createErr)
			return
		}
		h.server.logAcceptedOperation(r, operation)
		writeJSON(w, http.StatusAccepted, map[string]any{"deployment": deployment, "operation": operation})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "id_generation_failed", "could not allocate a Deployment identifier", r)
}

func (h *generatedHandler) PreviewAppEnvironmentDeployment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	target, ok := h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	if !ok {
		return
	}
	var input struct {
		ReleaseID            string `json:"releaseId"`
		ConfigurationVersion int64  `json:"configurationVersion"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return
	}
	preview, err := h.server.store.PreviewDeployment(r.Context(), workspace.ID, target.ID, input.ReleaseID, input.ConfigurationVersion)
	if err != nil {
		writeAppEnvironmentError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *generatedHandler) ListAppEnvironmentConfigurationVersions(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, params generated.ListAppEnvironmentConfigurationVersionsParams) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	target, ok := h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	if !ok {
		return
	}
	beforeVersion, limit, ok := hierarchyPage(w, r, params.Cursor, params.Limit)
	if !ok {
		return
	}
	items, nextCursor, err := h.server.store.ListConfigurationRevisions(r.Context(), workspace.ID, target.ID, beforeVersion, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "could not list configuration versions", r)
		return
	}
	writeHierarchyList(w, items, nextCursor)
}

func (h *generatedHandler) GetAppEnvironmentDeployment(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, projectID generated.ProjectId, appID generated.AppId, appEnvironmentID generated.AppEnvironmentId, deploymentID generated.DeploymentId) {
	_, workspace, ok := h.authorizeWorkspace(w, r, string(workspaceID), false)
	if !ok {
		return
	}
	appEnvironment, ok := h.appEnvironment(w, r, workspace.ID, string(projectID), string(appID), string(appEnvironmentID))
	if !ok {
		return
	}
	deployment, err := h.server.store.FindDeployment(r.Context(), workspace.ID, appEnvironment.ID, string(deploymentID))
	if err != nil {
		writeAppEnvironmentError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deployment)
}

func (h *generatedHandler) appEnvironmentInput(w http.ResponseWriter, r *http.Request, requireEnvironment bool) (appEnvironmentInput, bool) {
	var input appEnvironmentInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid", r)
		return appEnvironmentInput{}, false
	}
	if requireEnvironment {
		if input.ClusterID == "" {
			writeError(w, http.StatusBadRequest, "cluster_required", "clusterId is required", r)
			return appEnvironmentInput{}, false
		}
		if err := domain.ValidateEnvironmentID(input.EnvironmentID); err != nil {
			writeError(w, http.StatusBadRequest, "environment_invalid", "environmentId is invalid", r)
			return appEnvironmentInput{}, false
		}
		if err := domain.ValidateWorkloadConfiguration(input.WorkloadKind, domain.NormalizeRuntimeConfig(input.Configuration), input.Volume); err != nil {
			writeError(w, http.StatusBadRequest, "workload_invalid", err.Error(), r)
			return appEnvironmentInput{}, false
		}
	}
	if input.Branch != "" {
		branch, err := domain.NormalizeSourceBranch(input.Branch)
		if err != nil {
			writeError(w, http.StatusBadRequest, "branch_invalid", err.Error(), r)
			return appEnvironmentInput{}, false
		}
		input.Branch = branch
	}
	input.Configuration = domain.NormalizeRuntimeConfig(input.Configuration)
	if err := domain.ValidateRuntimeConfig(input.Configuration, h.server.config.MaxReplicas, h.server.config.MaxCPU, h.server.config.MaxMemory); err != nil {
		writeError(w, http.StatusBadRequest, "configuration_invalid", err.Error(), r)
		return appEnvironmentInput{}, false
	}
	for _, endpoint := range input.Configuration.PublicEndpoints {
		if endpoint.Type == domain.EndpointTCP && !h.server.config.PublicTCPEnabled {
			writeError(w, http.StatusConflict, "public_tcp_unavailable", "public TCP endpoints are unavailable in this installation", r)
			return appEnvironmentInput{}, false
		}
	}
	return input, true
}

func (h *generatedHandler) appEnvironment(w http.ResponseWriter, r *http.Request, workspaceID int64, projectID, appID, publicID string) (domain.AppEnvironment, bool) {
	item, err := h.server.store.FindAppEnvironmentForApp(r.Context(), workspaceID, projectID, appID, publicID)
	if err != nil {
		writeAppEnvironmentError(w, r, err)
		return domain.AppEnvironment{}, false
	}
	return item, true
}

func writeAppEnvironmentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, kubernetesbinding.ErrHTTPSelectionRequired), errors.Is(err, kubernetesbinding.ErrHTTPDestinationUnavailable), errors.Is(err, domain.ErrPublicationUnsupported), errors.Is(err, domain.ErrPublicationNotGranted), errors.Is(err, domain.ErrPublicationName), errors.Is(err, domain.ErrPublicationReserved), errors.Is(err, domain.ErrPublicationLimit), errors.Is(err, store.ErrPublicationDependency):
		writePublicationError(w, r, err)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "app_environment_not_found", "App Environment was not found", r)
	case errors.Is(err, store.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "App Environment changed since it was read", r)
	case errors.Is(err, store.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with a different request", r)
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "app_environment_conflict", "App Environment conflicts with existing state", r)
	case errors.Is(err, store.ErrParameterBinding):
		writeError(w, http.StatusBadRequest, "parameter_binding_invalid", "a Parameter binding is unavailable in this Workspace", r)
	case errors.Is(err, store.ErrStorageProfileUnavailable):
		writeError(w, http.StatusConflict, "storage_profile_unavailable", "the selected storage profile is unavailable", r)
	case errors.Is(err, store.ErrStorageQuotaExceeded):
		writeError(w, http.StatusConflict, "storage_quota_exceeded", "the requested storage capacity is unavailable", r)
	case errors.Is(err, store.ErrPublicationConflict):
		writeError(w, http.StatusConflict, "public_endpoint_conflict", "the requested public endpoint is unavailable", r)
	case errors.Is(err, store.ErrPublicationUnavailable):
		writeError(w, http.StatusConflict, "public_tcp_unavailable", "public TCP endpoint capacity is unavailable", r)
	case errors.Is(err, store.ErrPublicationDomainNotAllowed):
		writeError(w, http.StatusBadRequest, "publication_domain_not_allowed", "the selected publication domain is not allowed for this workload", r)
	case errors.Is(err, store.ErrPublicationHostnameReserved):
		writeError(w, http.StatusBadRequest, "publication_hostname_reserved", "the requested public hostname is reserved", r)
	case errors.Is(err, store.ErrAgentUnavailable):
		writeError(w, http.StatusConflict, "cluster_unavailable", "the selected cluster is unavailable or the workspace is not ready on it", r)
	default:
		writeError(w, http.StatusInternalServerError, "storage_failed", "App Environment state could not be persisted", r)
	}
}
