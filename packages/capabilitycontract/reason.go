package capabilitycontract

const (
	ReasonAccessDenied             = "access_denied"
	ReasonAPIMissing               = "api_missing"
	ReasonNoResource               = "no_resource"
	ReasonReadabilityNotVerified   = "readability_not_verified"
	ReasonProbeFailed              = "probe_failed"
	ReasonClusterNotAttached       = "cluster_not_attached"
	ReasonClusterAgentOffline      = "cluster_agent_offline"
	ReasonClusterObservationStale  = "cluster_observation_stale"
	ReasonCapabilityIncompatible   = "cluster_capability_incompatible"
	ReasonProviderNotConfigured    = "provider_not_configured"
	ReasonProviderHealthUnknown    = "provider_health_unknown"
	ReasonProviderUnreachable      = "provider_unreachable"
	ReasonBindingDegraded          = "binding_degraded"
	ReasonHistoricalBackendMissing = "historical_backend_missing"
	ReasonSecretBackendMissing     = "secret_backend_missing"
	ReasonRuntimeQueryUnsupported  = "runtime_query_unsupported"
)
