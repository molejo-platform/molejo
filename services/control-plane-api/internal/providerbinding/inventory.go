// Package providerbinding contains typed, read-only external provider facts.
package providerbinding

import (
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

type Health string

const (
	HealthUnknown     Health = "Unknown"
	HealthHealthy     Health = "Healthy"
	HealthDegraded    Health = "Degraded"
	HealthUnavailable Health = "Unavailable"
)

type Binding struct {
	Capability          capabilitycontract.ID
	Configured          bool
	Health              Health
	ConformanceRequired bool
	Conformant          bool
	ReasonCode          string
	Limitations         []string
	ObservedAt          *time.Time
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

func (i Inventory) With(bindings ...Binding) Inventory {
	values := make([]Binding, 0, len(i.bindings)+len(bindings))
	for _, binding := range i.bindings {
		values = append(values, binding)
	}
	values = append(values, bindings...)
	return New(values...)
}
