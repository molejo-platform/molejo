// Package featureavailability derives structural product availability from
// explicit facts. Resolution is pure and never probes external systems.
package featureavailability

import (
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/providerbinding"
)

type State string

const (
	Available     State = "Available"
	Limited       State = "Limited"
	NotConfigured State = "NotConfigured"
	Unavailable   State = "Unavailable"
	Unsupported   State = "Unsupported"
	Unknown       State = "Unknown"
)

const (
	ReasonClusterNotAttached            = capabilitycontract.ReasonClusterNotAttached
	ReasonClusterAgentOffline           = capabilitycontract.ReasonClusterAgentOffline
	ReasonClusterObservationStale       = capabilitycontract.ReasonClusterObservationStale
	ReasonClusterCapabilityMissing      = "cluster_capability_missing"
	ReasonClusterCapabilityIncompatible = capabilitycontract.ReasonCapabilityIncompatible
	ReasonProviderNotConfigured         = capabilitycontract.ReasonProviderNotConfigured
	ReasonProviderHealthUnknown         = capabilitycontract.ReasonProviderHealthUnknown
	ReasonProviderUnreachable           = capabilitycontract.ReasonProviderUnreachable
	ReasonBindingMissing                = "binding_missing"
	ReasonBindingDegraded               = capabilitycontract.ReasonBindingDegraded
	ReasonMetricsAPIMissing             = "metrics_api_missing"
	ReasonRuntimeNotDeployed            = capabilitycontract.ReasonRuntimeNotDeployed
	ReasonAppSourceMissing              = "app_source_missing"
	ReasonHistoricalBackendMissing      = capabilitycontract.ReasonHistoricalBackendMissing
	ReasonSecretBackendMissing          = capabilitycontract.ReasonSecretBackendMissing
	ReasonRuntimeQueryUnsupported       = capabilitycontract.ReasonRuntimeQueryUnsupported
)

type ScopeType string

const (
	ScopeWorkspace      ScopeType = "Workspace"
	ScopeApp            ScopeType = "App"
	ScopeAppEnvironment ScopeType = "AppEnvironment"
)

type Target struct {
	ScopeType ScopeType
	ScopeID   string
	ClusterID string
}

type Facts struct {
	ClusterAttached      bool
	AgentConnected       bool
	ProtocolCapabilities []string
	Observations         []capabilitycontract.Observation
	Providers            providerbinding.Inventory
}

type Feature struct {
	ID              capabilitycontract.ID `json:"id"`
	ContractVersion string                `json:"contractVersion"`
	State           State                 `json:"state"`
	ReasonCode      string                `json:"reasonCode,omitempty"`
	Limitations     []string              `json:"limitations"`
	ObservedAt      *time.Time            `json:"observedAt,omitempty"`
}
