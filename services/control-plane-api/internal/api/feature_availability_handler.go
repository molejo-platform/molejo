package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/featureavailability"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/historicalmetrics"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
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
	if facts.Attached {
		storageBindings, bindingErr := h.server.store.ClusterStorageBindings(r.Context(), facts.ClusterID)
		if bindingErr != nil {
			writeError(w, http.StatusInternalServerError, "storage_failed", "feature availability could not be loaded", r)
			return
		}
		providers = providers.With(storageBindingFacts(storageBindings, now)...)
		publicationBinding, bindingErr := h.server.store.ClusterPublicationBinding(r.Context(), facts.ClusterID)
		switch {
		case bindingErr == nil:
			providers = providers.With(publicationBindingFact(publicationBinding, now))
		case !errors.Is(bindingErr, store.ErrBindingNotFound):
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

func storageBindingFacts(bindings []store.ClusterStorageBinding, now time.Time) []providerbinding.Binding {
	rwo := providerbinding.Binding{Capability: capabilitycontract.StorageRWO}
	expand := providerbinding.Binding{Capability: capabilitycontract.StorageExpand}
	for _, binding := range bindings {
		rwo.Configured, expand.Configured = true, true
		health, reason := providerBindingHealth(binding.Health, binding.ReasonCode, binding.ExpiresAt, now)
		if providerHealthRank(health) > providerHealthRank(rwo.Health) {
			rwo.Health, rwo.ReasonCode, rwo.ObservedAt = health, reason, binding.ObservedAt
		}
		if binding.AllowExpansion && providerHealthRank(health) > providerHealthRank(expand.Health) {
			expand.Health, expand.ReasonCode, expand.ObservedAt = health, reason, binding.ObservedAt
		}
	}
	if expand.Configured && expand.Health == "" {
		expand.Health, expand.ReasonCode = providerbinding.HealthUnavailable, "storage_expansion_unsupported"
	}
	return []providerbinding.Binding{rwo, expand}
}

func publicationBindingFact(binding store.ClusterPublicationBinding, now time.Time) providerbinding.Binding {
	health, reason := providerBindingHealth(binding.Health, binding.ReasonCode, binding.ExpiresAt, now)
	return providerbinding.Binding{Capability: capabilitycontract.PublicationHTTP, Configured: true, Health: health, ReasonCode: reason, ObservedAt: binding.ObservedAt}
}

func providerBindingHealth(health kubernetesbinding.Health, reason string, expiresAt *time.Time, now time.Time) (providerbinding.Health, string) {
	if expiresAt == nil || !expiresAt.After(now) {
		return providerbinding.HealthUnknown, "binding_observation_stale"
	}
	return providerbinding.Health(health), reason
}

func providerHealthRank(health providerbinding.Health) int {
	switch health {
	case providerbinding.HealthHealthy:
		return 4
	case providerbinding.HealthDegraded:
		return 3
	case providerbinding.HealthUnavailable:
		return 2
	case providerbinding.HealthUnknown:
		return 1
	default:
		return 0
	}
}
