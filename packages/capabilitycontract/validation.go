package capabilitycontract

import (
	"errors"
	"strings"
	"time"
)

const (
	MaxObservations         = 64
	MaxFieldLength          = 128
	MaxMessageLength        = 256
	MaxLimitations          = 16
	MaxLimitationLength     = 128
	MaxObservationClockSkew = 10 * time.Minute
)

var ErrInvalidObservation = errors.New("invalid capability observation")

func ValidateObservation(value Observation, now time.Time) error {
	if !Known(value.ID) || value.ContractVersion != ContractVersion || !validSupport(value.Support) || !validHealth(value.Health) {
		return ErrInvalidObservation
	}
	if tooLong(value.ProviderKind, MaxFieldLength) || tooLong(value.ReasonCode, MaxFieldLength) || tooLong(value.Message, MaxMessageLength) || len(value.Limitations) > MaxLimitations {
		return ErrInvalidObservation
	}
	for _, limitation := range value.Limitations {
		if strings.TrimSpace(limitation) == "" || tooLong(limitation, MaxLimitationLength) {
			return ErrInvalidObservation
		}
	}
	if value.SampledAt.IsZero() || value.SampledAt.After(now.Add(MaxObservationClockSkew)) {
		return ErrInvalidObservation
	}
	return nil
}

func validSupport(value Support) bool {
	return value == SupportSupported || value == SupportUnsupported || value == SupportUnknown
}

func validHealth(value Health) bool {
	return value == HealthHealthy || value == HealthDegraded || value == HealthUnavailable || value == HealthUnknown
}

func tooLong(value string, maximum int) bool { return len(strings.TrimSpace(value)) > maximum }
