package capabilitycontract

import "time"

type Support string

const (
	SupportSupported   Support = "Supported"
	SupportUnsupported Support = "Unsupported"
	SupportUnknown     Support = "Unknown"
)

type Health string

const (
	HealthHealthy     Health = "Healthy"
	HealthDegraded    Health = "Degraded"
	HealthUnavailable Health = "Unavailable"
	HealthUnknown     Health = "Unknown"
)

type Observation struct {
	ID              ID
	ContractVersion string
	Support         Support
	Health          Health
	ProviderKind    string
	ReasonCode      string
	Message         string
	Limitations     []string
	SampledAt       time.Time
	ReceivedAt      time.Time
	ExpiresAt       time.Time
}
