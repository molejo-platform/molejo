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
		case capabilitycontract.RuntimeLogsCurrent, capabilitycontract.RuntimeMetricsCurrent, capabilitycontract.RuntimeEventsCurrent:
			feature.State, feature.ReasonCode = Unsupported, ReasonRuntimeQueryUnsupported
		default:
			feature = resolveProvider(now, feature, facts)
		}
		features = append(features, feature)
	}
	sort.Slice(features, func(i, j int) bool { return features[i].ID < features[j].ID })
	return features
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
	if !contains(facts.ProtocolCapabilities, "runtime.v1alpha1") {
		feature.State, feature.ReasonCode = Unsupported, ReasonClusterCapabilityIncompatible
		return feature
	}
	feature.State = Available
	return feature
}

func resolveProvider(now time.Time, feature Feature, facts Facts) Feature {
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
		feature.Limitations = append([]string(nil), observation.Limitations...)
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
	binding, ok := facts.Providers.Find(feature.ID)
	if !ok || !binding.Configured {
		feature.State, feature.ReasonCode = NotConfigured, missingProviderReason(feature.ID)
		return feature
	}
	switch binding.Health {
	case providerbinding.HealthHealthy:
		feature.State = Available
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
