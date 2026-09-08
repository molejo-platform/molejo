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
	ReasonClusterNotAttached            = "cluster_not_attached"
	ReasonClusterAgentOffline           = "cluster_agent_offline"
	ReasonClusterObservationStale       = "cluster_observation_stale"
	ReasonClusterCapabilityMissing      = "cluster_capability_missing"
	ReasonClusterCapabilityIncompatible = "cluster_capability_incompatible"
	ReasonProviderNotConfigured         = "provider_not_configured"
	ReasonProviderHealthUnknown         = "provider_health_unknown"
	ReasonProviderUnreachable           = "provider_unreachable"
	ReasonBindingMissing                = "binding_missing"
	ReasonBindingDegraded               = "binding_degraded"
	ReasonMetricsAPIMissing             = "metrics_api_missing"
	ReasonRuntimeNotDeployed            = "runtime_not_deployed"
	ReasonAppSourceMissing              = "app_source_missing"
	ReasonHistoricalBackendMissing      = "historical_backend_missing"
	ReasonSecretBackendMissing          = "secret_backend_missing"
	ReasonRuntimeQueryUnsupported       = "runtime_query_unsupported"
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
