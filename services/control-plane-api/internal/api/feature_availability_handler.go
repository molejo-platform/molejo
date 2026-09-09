package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/featureavailability"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/historicalmetrics"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

const agentFreshness = 30 * time.Second

func (h *generatedHandler) GetFeatureAvailability(w http.ResponseWriter, r *http.Request, workspaceID generated.WorkspaceId, params generated.GetFeatureAvailabilityParams) {
	_, workspace, ok := h.authorizeWorkspacePermission(w, r, string(workspaceID), false, authorization.ReadWorkspace)
	if !ok {
		return
	}
	scopeType := string(params.ScopeType)
	facts, err := h.server.store.FeatureAvailabilityClusterFacts(r.Context(), workspace.ID, scopeType, params.ScopeId)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource was not found", r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_failed", "feature availability could not be loaded", r)
		return
	}
	now := time.Now().UTC()
	providers := h.server.providerInventory
	if facts.Attached && h.server.historicalMetricBindings != nil {
		binding, bindingErr := h.server.historicalMetricBindings.Get(r.Context(), facts.ClusterID)
		switch {
		case bindingErr == nil:
			providers = providers.With(historicalmetrics.ProviderFact(binding))
		case !errors.Is(bindingErr, historicalmetrics.ErrNotFound):
			writeError(w, http.StatusInternalServerError, "storage_failed", "feature availability could not be loaded", r)
			return
		}
	}
	observations := []capabilitycontract.Observation{}
	if facts.Attached {
		observations, err = h.server.store.CapabilityObservations(r.Context(), facts.ClusterID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "storage_failed", "feature availability could not be loaded", r)
			return
		}
	}
	resolved := featureavailability.Resolve(now, featureavailability.Target{ScopeType: featureavailability.ScopeType(scopeType), ScopeID: params.ScopeId, ClusterID: facts.ClusterID}, featureavailability.Facts{
		ClusterAttached:           facts.Attached,
		AgentConnected:            facts.LastSeenAt != nil && facts.LastSeenAt.After(now.Add(-agentFreshness)),
		ProtocolCapabilities:      facts.Capabilities,
		WorkspaceProvisioningMode: facts.WorkspaceProvisioningMode,
		Observations:              observations,
		Providers:                 providers,
	})
	features := make([]generated.FeatureAvailability, 0, len(resolved))
	for _, value := range resolved {
		feature := generated.FeatureAvailability{Id: string(value.ID), ContractVersion: value.ContractVersion, State: generated.FeatureAvailabilityState(value.State), Limitations: value.Limitations}
		if value.ReasonCode != "" {
			feature.ReasonCode = &value.ReasonCode
		}
		if value.ObservedAt != nil {
			observed := value.ObservedAt.UTC()
			feature.ObservedAt = &observed
		}
		features = append(features, feature)
	}
	writeJSON(w, http.StatusOK, generated.FeatureAvailabilityResponse{ScopeType: generated.FeatureAvailabilityResponseScopeType(scopeType), ScopeId: params.ScopeId, Features: features})
}
