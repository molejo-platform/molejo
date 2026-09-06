// Package capability defines the common language used by cluster capability runbooks.
package capability

// Ownership describes who is responsible for the lifecycle of a capability.
type Ownership string

const (
	OwnershipExternal        Ownership = "external"
	OwnershipRunbookManaged  Ownership = "runbook-managed"
	OwnershipMolejoManaged   Ownership = "molejo-managed"
	OwnershipProviderManaged Ownership = "provider-managed"
)

// Status is the observed availability of a capability.
type Status string

const (
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
	StatusDegraded    Status = "degraded"
	StatusUnknown     Status = "unknown"
)

// Observation is a read-only capability fact reported by Molejo.
type Observation struct {
	Name      string
	Status    Status
	Ownership Ownership
	Provider  string
	Detail    string
}
