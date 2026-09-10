// Package workspaceprovisioning owns the policy for admitting a Workspace
// placement. It is deliberately free of HTTP, persistence, and Kubernetes I/O.
package workspaceprovisioning

import (
	"regexp"
	"strings"

	"github.com/molejo-platform/molejo/packages/workspacecontract"
)

type Consent = workspacecontract.ProvisioningMode

const (
	ConsentDisabled   = workspacecontract.ProvisioningDisabled
	ConsentNamespaced = workspacecontract.ProvisioningNamespaced
)

type Reason string

const (
	ReasonAccepted              Reason = "accepted"
	ReasonActorUnauthorized     Reason = "actor_unauthorized"
	ReasonClusterNotAttached    Reason = "cluster_not_attached"
	ReasonCapabilityUnavailable Reason = "capability_unavailable"
	ReasonConsentDisabled       Reason = "cluster_consent_disabled"
	ReasonNamespaceReserved     Reason = "namespace_reserved"
	ReasonNamespaceInvalid      Reason = "namespace_invalid"
	ReasonIdempotencyRequired   Reason = "idempotency_required"
)

type Input struct {
	ActorAuthorized     bool
	ClusterAttached     bool
	CapabilityAvailable bool
	Consent             Consent
	Namespace           string
	IdempotencyPresent  bool
}

type Decision struct {
	Accepted bool
	Reason   Reason
}

var namespacePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)

func ParseConsent(value string) (Consent, bool) {
	return workspacecontract.ParseProvisioningMode(value)
}

func Decide(input Input) Decision {
	switch {
	case !input.ActorAuthorized:
		return Decision{Reason: ReasonActorUnauthorized}
	case !input.ClusterAttached:
		return Decision{Reason: ReasonClusterNotAttached}
	case !input.CapabilityAvailable:
		return Decision{Reason: ReasonCapabilityUnavailable}
	case input.Consent != ConsentNamespaced:
		return Decision{Reason: ReasonConsentDisabled}
	case reservedNamespace(input.Namespace):
		return Decision{Reason: ReasonNamespaceReserved}
	case !namespacePattern.MatchString(input.Namespace):
		return Decision{Reason: ReasonNamespaceInvalid}
	case !input.IdempotencyPresent:
		return Decision{Reason: ReasonIdempotencyRequired}
	default:
		return Decision{Accepted: true, Reason: ReasonAccepted}
	}
}

func reservedNamespace(namespace string) bool {
	return namespace == "default" || namespace == "molejo-system" || strings.HasPrefix(namespace, "kube-")
}
