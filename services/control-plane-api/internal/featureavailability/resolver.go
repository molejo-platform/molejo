package featureavailability

import (
	"sort"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
)

func Resolve(now time.Time, target Target, facts Facts) []Feature {
	features := make([]Feature, 0, len(catalog))
	for _, id := range catalog {
		feature := Feature{ID: id, ContractVersion: capabilitycontract.ContractVersion, Limitations: []string{}}
		switch id {
		case capabilitycontract.ReleaseExternal, capabilitycontract.ReleaseHistory, capabilitycontract.ParametersPlain, capabilitycontract.ControlPlaneOperationEvents:
			feature.State = Available
		case capabilitycontract.RuntimeWorkloadApply, capabilitycontract.RuntimeWorkloadObserve:
			feature = resolveRuntime(feature, facts)
		case capabilitycontract.WorkspaceProvisioning:
			feature = resolveWorkspaceProvisioning(feature, facts)
		case capabilitycontract.RuntimeLogsCurrent, capabilitycontract.RuntimeMetricsCurrent, capabilitycontract.RuntimeEventsCurrent:
			feature = resolveRuntimeQuery(now, feature, facts)
		default:
			feature = resolveProvider(now, feature, facts)
		}
		features = append(features, feature)
	}
	sort.Slice(features, func(i, j int) bool { return features[i].ID < features[j].ID })
	return features
}

func resolveWorkspaceProvisioning(feature Feature, facts Facts) Feature {
	feature = resolveRuntime(feature, facts)
	if feature.State != Available {
		return feature
	}
	if !contains(facts.ProtocolCapabilities, "workspace-provisioning.v1alpha1") {
		feature.State, feature.ReasonCode = Unsupported, ReasonClusterCapabilityIncompatible
		return feature
	}
	if facts.WorkspaceProvisioningMode != "Namespaced" {
		feature.State, feature.ReasonCode = NotConfigured, ReasonWorkspaceProvisioningDisabled
	}
	return feature
}

func resolveRuntimeQuery(now time.Time, feature Feature, facts Facts) Feature {
	if !facts.ClusterAttached {
		feature.State, feature.ReasonCode = NotConfigured, ReasonClusterNotAttached
		return feature
	}
	if !facts.AgentConnected {
		feature.State, feature.ReasonCode = Unknown, ReasonClusterAgentOffline
		return feature
	}
	if !contains(facts.ProtocolCapabilities, "runtime-query.v1alpha1") {
		feature.State, feature.ReasonCode = Unsupported, ReasonRuntimeQueryUnsupported
		return feature
	}
	if _, ok := latestObservationRegardlessFreshness(feature.ID, facts.Observations); !ok {
		feature.State, feature.ReasonCode = Unknown, ReasonClusterObservationStale
		return feature
	}
	return resolveProvider(now, feature, facts)
}

func resolveRuntime(feature Feature, facts Facts) Feature {
	if !facts.ClusterAttached {
		feature.State, feature.ReasonCode = NotConfigured, ReasonClusterNotAttached
		return feature
	}
	if !facts.AgentConnected {
		feature.State, feature.ReasonCode = Unknown, ReasonClusterAgentOffline
		return feature
	}
	if !contains(facts.ProtocolCapabilities, "runtime.v1alpha2") {
		feature.State, feature.ReasonCode = Unsupported, ReasonClusterCapabilityIncompatible
		return feature
	}
	feature.State = Available
	return feature
}

func resolveProvider(now time.Time, feature Feature, facts Facts) Feature {
	if feature.ID == capabilitycontract.TelemetryMetricsHistorical || feature.ID == capabilitycontract.StorageRWO || feature.ID == capabilitycontract.StorageExpand || feature.ID == capabilitycontract.PublicationHTTP {
		return resolveExplicitBinding(feature, facts)
	}
	if observation, ok := latestObservationRegardlessFreshness(feature.ID, facts.Observations); ok {
		if !facts.AgentConnected {
			feature.State, feature.ReasonCode = Unknown, ReasonClusterAgentOffline
			return feature
		}
		if !observation.ExpiresAt.IsZero() && !observation.ExpiresAt.After(now) {
			feature.State, feature.ReasonCode = Unknown, ReasonClusterObservationStale
			feature.ObservedAt = timePointer(observation.SampledAt)
			return feature
		}
	}
	if observation, ok := latestObservation(now, feature.ID, facts.Observations); ok {
		feature.ObservedAt = timePointer(observation.SampledAt)
		feature.Limitations = append([]string{}, observation.Limitations...)
		switch {
		case observation.Support == capabilitycontract.SupportUnsupported:
			feature.State, feature.ReasonCode = Unsupported, observation.ReasonCode
		case observation.Support == capabilitycontract.SupportUnknown || observation.Health == capabilitycontract.HealthUnknown:
			feature.State, feature.ReasonCode = Unknown, observation.ReasonCode
		case observation.Health == capabilitycontract.HealthHealthy && len(observation.Limitations) > 0:
			feature.State, feature.ReasonCode = Limited, observation.ReasonCode
		case observation.Health == capabilitycontract.HealthHealthy:
			feature.State = Available
		case observation.Health == capabilitycontract.HealthDegraded:
			feature.State, feature.ReasonCode = Limited, observation.ReasonCode
		default:
			feature.State, feature.ReasonCode = Unavailable, observation.ReasonCode
		}
		return feature
	}
	return resolveExplicitBinding(feature, facts)
}

func resolveExplicitBinding(feature Feature, facts Facts) Feature {
	binding, ok := facts.Providers.Find(feature.ID)
	if !ok || !binding.Configured {
		feature.State, feature.ReasonCode = NotConfigured, missingProviderReason(feature.ID)
		return feature
	}
	feature.Limitations = append([]string{}, binding.Limitations...)
	if binding.ObservedAt != nil {
		observed := binding.ObservedAt.UTC()
		feature.ObservedAt = &observed
	}
	switch binding.Health {
	case providerbinding.HealthHealthy:
		if binding.ConformanceRequired && !binding.Conformant {
			feature.State, feature.ReasonCode = Unknown, first(binding.ReasonCode, ReasonProviderHealthUnknown)
		} else if len(binding.Limitations) > 0 {
			feature.State, feature.ReasonCode = Limited, binding.ReasonCode
		} else {
			feature.State = Available
		}
	case providerbinding.HealthDegraded:
		feature.State, feature.ReasonCode = Limited, first(binding.ReasonCode, ReasonBindingDegraded)
	case providerbinding.HealthUnavailable:
		feature.State, feature.ReasonCode = Unavailable, first(binding.ReasonCode, ReasonProviderUnreachable)
	default:
		feature.State, feature.ReasonCode = Unknown, first(binding.ReasonCode, ReasonProviderHealthUnknown)
	}
	return feature
}

func latestObservationRegardlessFreshness(id capabilitycontract.ID, observations []capabilitycontract.Observation) (capabilitycontract.Observation, bool) {
	var latest capabilitycontract.Observation
	found := false
	for _, observation := range observations {
		if observation.ID != id || observation.ContractVersion != capabilitycontract.ContractVersion {
			continue
		}
		if !found || observation.ReceivedAt.After(latest.ReceivedAt) {
			latest, found = observation, true
		}
	}
	return latest, found
}

func latestObservation(now time.Time, id capabilitycontract.ID, observations []capabilitycontract.Observation) (capabilitycontract.Observation, bool) {
	var latest capabilitycontract.Observation
	found := false
	for _, observation := range observations {
		if observation.ID != id || observation.ContractVersion != capabilitycontract.ContractVersion || (!observation.ExpiresAt.IsZero() && !observation.ExpiresAt.After(now)) {
			continue
		}
		if !found || observation.ReceivedAt.After(latest.ReceivedAt) {
			latest, found = observation, true
		}
	}
	return latest, found
}

func missingProviderReason(id capabilitycontract.ID) string {
	switch id {
	case capabilitycontract.StorageRWO, capabilitycontract.StorageExpand, capabilitycontract.PublicationHTTP:
		return ReasonBindingMissing
	case capabilitycontract.ParametersSecretStatic:
		return ReasonSecretBackendMissing
	case capabilitycontract.TelemetryLogsHistorical, capabilitycontract.TelemetryMetricsHistorical, capabilitycontract.TelemetryEventsHistorical:
		return ReasonHistoricalBackendMissing
	default:
		return ReasonProviderNotConfigured
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func first(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
func timePointer(value time.Time) *time.Time { value = value.UTC(); return &value }
