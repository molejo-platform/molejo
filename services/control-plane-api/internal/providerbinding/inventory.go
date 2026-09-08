// Package providerbinding contains typed, read-only external provider facts.
package providerbinding

import "github.com/molejo-platform/molejo/packages/capabilitycontract"

type Health string

const (
	HealthUnknown     Health = "Unknown"
	HealthHealthy     Health = "Healthy"
	HealthDegraded    Health = "Degraded"
	HealthUnavailable Health = "Unavailable"
)

type Binding struct {
	Capability capabilitycontract.ID
	Configured bool
	Health     Health
	ReasonCode string
}

type Inventory struct {
	bindings map[capabilitycontract.ID]Binding
}

func New(bindings ...Binding) Inventory {
	values := make(map[capabilitycontract.ID]Binding, len(bindings))
	for _, binding := range bindings {
		values[binding.Capability] = binding
	}
	return Inventory{bindings: values}
}

func (i Inventory) Find(id capabilitycontract.ID) (Binding, bool) {
	value, ok := i.bindings[id]
	return value, ok
}
