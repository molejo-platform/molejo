package capabilitycontract

import (
	"errors"
	"testing"
	"time"
)

func TestValidateObservation(t *testing.T) {
	now := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	valid := Observation{ID: StorageRWO, ContractVersion: ContractVersion, Support: SupportSupported, Health: HealthHealthy, SampledAt: now}
	if err := ValidateObservation(valid, now); err != nil {
		t.Fatalf("valid observation: %v", err)
	}
	invalid := valid
	invalid.ID = "provider.loki"
	if err := ValidateObservation(invalid, now); !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("got %v, want ErrInvalidObservation", err)
	}
}
