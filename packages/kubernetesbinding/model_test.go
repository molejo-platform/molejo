package kubernetesbinding

import (
	"testing"
	"time"
)

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		valid  bool
	}{
		{name: "storage", target: Target{ID: "storage:persistent-standard", Kind: KindStorage, Version: 1, Storage: &StorageTarget{StorageClassName: "local-path"}}, valid: true},
		{name: "publication", target: Target{ID: "publication:http", Kind: KindPublicationHTTP, Version: 2, Publication: &PublicationTarget{GatewayNamespace: "molejo-system", GatewayName: "molejo", SectionName: "https-molejo"}}, valid: true},
		{name: "missing version", target: Target{ID: "storage:standard", Kind: KindStorage, Storage: &StorageTarget{StorageClassName: "standard"}}},
		{name: "wrong payload", target: Target{ID: "storage:standard", Kind: KindStorage, Version: 1, Publication: &PublicationTarget{GatewayNamespace: "molejo-system", GatewayName: "molejo", SectionName: "https-molejo"}}},
		{name: "unsafe gateway namespace", target: Target{ID: "publication:http", Kind: KindPublicationHTTP, Version: 1, Publication: &PublicationTarget{GatewayNamespace: "../system", GatewayName: "molejo", SectionName: "https"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateTarget(test.target); (got == nil) != test.valid {
				t.Fatalf("ValidateTarget() error = %v, valid = %t", got, test.valid)
			}
		})
	}
}

func TestValidateObservationRequiresMatchingTypedEvidence(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	target := Target{ID: "storage:persistent-standard", Kind: KindStorage, Version: 3, Storage: &StorageTarget{StorageClassName: "local-path"}}
	valid := Observation{ID: target.ID, Kind: target.Kind, Version: target.Version, Health: HealthHealthy, SampledAt: now, Storage: &StorageObservation{StorageClassName: "local-path", Provisioner: "rancher.io/local-path", AccessModes: []string{"ReadWriteOnce"}, VolumeBindingMode: "WaitForFirstConsumer"}}
	if err := ValidateObservation(target, valid, now); err != nil {
		t.Fatalf("ValidateObservation() error = %v", err)
	}
	for name, mutate := range map[string]func(*Observation){
		"binding version": func(value *Observation) { value.Version++ },
		"class identity":  func(value *Observation) { value.Storage.StorageClassName = "other" },
		"future sample":   func(value *Observation) { value.SampledAt = now.Add(2 * time.Minute) },
		"duplicate modes": func(value *Observation) { value.Storage.AccessModes = []string{"ReadWriteOnce", "ReadWriteOnce"} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			storage := *valid.Storage
			storage.AccessModes = append([]string{}, valid.Storage.AccessModes...)
			candidate.Storage = &storage
			mutate(&candidate)
			if err := ValidateObservation(target, candidate, now); err == nil {
				t.Fatal("ValidateObservation() expected an error")
			}
		})
	}
}

func TestAvailabilityExpiresObservation(t *testing.T) {
	now := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	observation := Observation{Health: HealthHealthy, SampledAt: now.Add(-ObservationTTL)}
	if got := EffectiveHealth(now, observation); got != HealthUnknown {
		t.Fatalf("EffectiveHealth() = %q, want %q", got, HealthUnknown)
	}
	observation.SampledAt = now.Add(-ObservationTTL + time.Second)
	if got := EffectiveHealth(now, observation); got != HealthHealthy {
		t.Fatalf("EffectiveHealth() = %q, want %q", got, HealthHealthy)
	}
}
