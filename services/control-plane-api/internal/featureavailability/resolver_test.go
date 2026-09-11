package featureavailability

import (
	"reflect"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
)

func TestResolveRuntimePrecedence(t *testing.T) {
	now := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		facts  Facts
		want   State
		reason string
	}{
		{"not attached", Facts{}, NotConfigured, ReasonClusterNotAttached},
		{"offline", Facts{ClusterAttached: true}, Unknown, ReasonClusterAgentOffline},
		{"old protocol", Facts{ClusterAttached: true, AgentConnected: true}, Unsupported, ReasonClusterCapabilityIncompatible},
		{"available", Facts{ClusterAttached: true, AgentConnected: true, ProtocolCapabilities: []string{"runtime.v1alpha3"}}, Available, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := featureByID(Resolve(now, Target{}, test.facts), capabilitycontract.RuntimeWorkloadApply)
			if got.State != test.want || got.ReasonCode != test.reason {
				t.Fatalf("got %s/%s, want %s/%s", got.State, got.ReasonCode, test.want, test.reason)
			}
		})
	}
}

func TestResolveProviderAndCopiesInputs(t *testing.T) {
	now := time.Now().UTC()
	limitations := []string{"single replica"}
	facts := Facts{AgentConnected: true, Providers: providerbinding.New(providerbinding.Binding{Capability: capabilitycontract.SourceGitHub, Configured: true}), Observations: []capabilitycontract.Observation{{ID: capabilitycontract.PublicationTCP, ContractVersion: capabilitycontract.ContractVersion, Support: capabilitycontract.SupportSupported, Health: capabilitycontract.HealthHealthy, Limitations: limitations, SampledAt: now, ReceivedAt: now, ExpiresAt: now.Add(time.Minute)}}}
	got := Resolve(now, Target{}, facts)
	if featureByID(got, capabilitycontract.SourceGitHub).State != Unknown {
		t.Fatal("configured provider without health proof must be Unknown")
	}
	storage := featureByID(got, capabilitycontract.PublicationTCP)
	if storage.State != Limited || !reflect.DeepEqual(storage.Limitations, limitations) {
		t.Fatalf("unexpected storage projection: %#v", storage)
	}
	storage.Limitations[0] = "changed"
	if limitations[0] != "single replica" {
		t.Fatal("output aliases input")
	}
	for index := 1; index < len(got); index++ {
		if got[index-1].ID > got[index].ID {
			t.Fatal("output is not sorted")
		}
	}
}

func TestResolveCurrentRuntimeRequiresQueryProtocolAndFreshObservation(t *testing.T) {
	now := time.Now().UTC()
	observation := capabilitycontract.Observation{ID: capabilitycontract.RuntimeLogsCurrent, ContractVersion: capabilitycontract.ContractVersion, Support: capabilitycontract.SupportSupported, Health: capabilitycontract.HealthHealthy, SampledAt: now, ReceivedAt: now, ExpiresAt: now.Add(time.Minute)}
	tests := []struct {
		name   string
		facts  Facts
		want   State
		reason string
	}{
		{name: "old Agent", facts: Facts{ClusterAttached: true, AgentConnected: true}, want: Unsupported, reason: ReasonRuntimeQueryUnsupported},
		{name: "missing observation", facts: Facts{ClusterAttached: true, AgentConnected: true, ProtocolCapabilities: []string{"runtime-query.v1alpha1"}}, want: Unknown, reason: ReasonClusterObservationStale},
		{name: "available", facts: Facts{ClusterAttached: true, AgentConnected: true, ProtocolCapabilities: []string{"runtime-query.v1alpha1"}, Observations: []capabilitycontract.Observation{observation}}, want: Available},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := featureByID(Resolve(now, Target{}, test.facts), capabilitycontract.RuntimeLogsCurrent)
			if got.State != test.want || got.ReasonCode != test.reason {
				t.Fatalf("got %s/%s, want %s/%s", got.State, got.ReasonCode, test.want, test.reason)
			}
		})
	}
}

func TestResolveObservationAlwaysReturnsLimitationsArray(t *testing.T) {
	now := time.Now().UTC()
	facts := Facts{
		AgentConnected: true,
		Observations: []capabilitycontract.Observation{{
			ID:              capabilitycontract.PublicationTCP,
			ContractVersion: capabilitycontract.ContractVersion,
			Support:         capabilitycontract.SupportSupported,
			Health:          capabilitycontract.HealthUnavailable,
			SampledAt:       now,
			ReceivedAt:      now,
			ExpiresAt:       now.Add(time.Minute),
		}},
	}

	feature := featureByID(Resolve(now, Target{}, facts), capabilitycontract.PublicationTCP)
	if feature.Limitations == nil {
		t.Fatal("observation-backed feature returned nil limitations")
	}
}

func TestResolveWorkspaceProvisioningKeepsConsentSeparateFromProtocolSupport(t *testing.T) {
	now := time.Now().UTC()
	base := Facts{ClusterAttached: true, AgentConnected: true, ProtocolCapabilities: []string{"runtime.v1alpha3", "workspace-provisioning.v1alpha1"}}
	disabled := featureByID(Resolve(now, Target{}, base), capabilitycontract.WorkspaceProvisioning)
	if disabled.State != NotConfigured || disabled.ReasonCode != ReasonWorkspaceProvisioningDisabled {
		t.Fatalf("disabled provisioning=%+v", disabled)
	}
	base.WorkspaceProvisioningMode = "Namespaced"
	available := featureByID(Resolve(now, Target{}, base), capabilitycontract.WorkspaceProvisioning)
	if available.State != Available || available.ReasonCode != "" {
		t.Fatalf("namespaced provisioning=%+v", available)
	}
}

func TestHistoricalMetricsRequireExplicitConformantBinding(t *testing.T) {
	now := time.Now().UTC()
	observed := capabilitycontract.Observation{ID: capabilitycontract.TelemetryMetricsHistorical, ContractVersion: capabilitycontract.ContractVersion, Support: capabilitycontract.SupportSupported, Health: capabilitycontract.HealthHealthy, SampledAt: now, ReceivedAt: now, ExpiresAt: now.Add(time.Minute)}
	tests := []struct {
		name    string
		binding *providerbinding.Binding
		want    State
		reason  string
	}{
		{name: "Agent observation cannot activate binding", want: NotConfigured, reason: ReasonHistoricalBackendMissing},
		{name: "configured but unproven", binding: &providerbinding.Binding{Capability: capabilitycontract.TelemetryMetricsHistorical, Configured: true, Health: providerbinding.HealthUnknown, ConformanceRequired: true}, want: Unknown, reason: ReasonProviderHealthUnknown},
		{name: "healthy and conformant", binding: &providerbinding.Binding{Capability: capabilitycontract.TelemetryMetricsHistorical, Configured: true, Health: providerbinding.HealthHealthy, ConformanceRequired: true, Conformant: true}, want: Available},
		{name: "unreachable", binding: &providerbinding.Binding{Capability: capabilitycontract.TelemetryMetricsHistorical, Configured: true, Health: providerbinding.HealthUnavailable, ConformanceRequired: true, ReasonCode: "metrics_provider_unreachable"}, want: Unavailable, reason: "metrics_provider_unreachable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory := providerbinding.New()
			if test.binding != nil {
				inventory = inventory.With(*test.binding)
			}
			feature := featureByID(Resolve(now, Target{}, Facts{AgentConnected: true, Observations: []capabilitycontract.Observation{observed}, Providers: inventory}), capabilitycontract.TelemetryMetricsHistorical)
			if feature.State != test.want || feature.ReasonCode != test.reason {
				t.Fatalf("got %s/%s, want %s/%s", feature.State, feature.ReasonCode, test.want, test.reason)
			}
		})
	}
}

func TestUnavailableHistoricalMetricsDoNotAffectApplicationLoop(t *testing.T) {
	now := time.Now().UTC()
	features := Resolve(now, Target{}, Facts{
		ClusterAttached:      true,
		AgentConnected:       true,
		ProtocolCapabilities: []string{"runtime.v1alpha3"},
		Providers: providerbinding.New(providerbinding.Binding{
			Capability: capabilitycontract.TelemetryMetricsHistorical,
			Configured: true,
			Health:     providerbinding.HealthUnavailable,
			ReasonCode: "metrics_provider_unreachable",
		}),
	})

	historical := featureByID(features, capabilitycontract.TelemetryMetricsHistorical)
	if historical.State != Unavailable || historical.ReasonCode != "metrics_provider_unreachable" {
		t.Fatalf("historical metrics=%+v", historical)
	}
	for _, id := range []capabilitycontract.ID{
		capabilitycontract.RuntimeWorkloadApply,
		capabilitycontract.RuntimeWorkloadObserve,
	} {
		if runtime := featureByID(features, id); runtime.State != Available {
			t.Fatalf("%s=%+v, want Available", id, runtime)
		}
	}
}

func TestKubernetesCapabilitiesRequireExplicitHealthyBindings(t *testing.T) {
	now := time.Now().UTC()
	for _, capability := range []capabilitycontract.ID{capabilitycontract.StorageRWO, capabilitycontract.PublicationHTTP} {
		t.Run(string(capability), func(t *testing.T) {
			observed := capabilitycontract.Observation{
				ID: capability, ContractVersion: capabilitycontract.ContractVersion,
				Support: capabilitycontract.SupportSupported, Health: capabilitycontract.HealthHealthy,
				SampledAt: now, ReceivedAt: now, ExpiresAt: now.Add(time.Minute),
			}
			withoutBinding := featureByID(Resolve(now, Target{}, Facts{AgentConnected: true, Observations: []capabilitycontract.Observation{observed}}), capability)
			if withoutBinding.State != NotConfigured || withoutBinding.ReasonCode != ReasonBindingMissing {
				t.Fatalf("observation activated %s without binding: %+v", capability, withoutBinding)
			}
			providers := providerbinding.New(providerbinding.Binding{Capability: capability, Configured: true, Health: providerbinding.HealthHealthy})
			withBinding := featureByID(Resolve(now, Target{}, Facts{AgentConnected: true, Providers: providers}), capability)
			if withBinding.State != Available {
				t.Fatalf("healthy binding did not activate %s: %+v", capability, withBinding)
			}
		})
	}
}

func featureByID(features []Feature, id capabilitycontract.ID) Feature {
	for _, feature := range features {
		if feature.ID == id {
			return feature
		}
	}
	return Feature{}
}
