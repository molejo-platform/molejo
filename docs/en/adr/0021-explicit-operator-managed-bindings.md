# ADR 0021: Explicit operator-managed bindings

## Status

Accepted for the alpha architecture.

## Context

Molejo must compose with infrastructure that a Cluster Operator already trusts
without silently selecting, installing, or reconfiguring that infrastructure.
Capability observations can prove that a StorageClass, Gateway, or telemetry
endpoint exists, but an observed candidate is not consent to use it and is not
durable product configuration.

## Decision

The Control Plane owns durable, typed capability bindings. A Cluster Operator
creates, changes, or deletes a binding explicitly through an authenticated
operator workflow. `molejoctl` may drive that workflow and run local validation,
but it does not make an observed candidate effective without operator intent.

The Cluster Agent reports authenticated, fresh evidence about the selected
cluster and the resources referenced by a binding. It never chooses or creates a
Control Plane binding. Discovery may present candidates, but activation remains
explicit.

Each capability receives a narrow binding contract when its first concrete slice
is implemented, such as historical metrics, storage, or publication. Molejo does
not introduce a universal provider registry, arbitrary configuration JSON, or a
generic plugin lifecycle. Provider credentials remain outside public API
responses and are represented only by the concrete binding's custody mechanism.

Feature Availability combines durable binding state with current Agent evidence.
A configured binding without fresh proof is `Unknown`; a healthy and conformant
binding is `Available`; an unreachable binding is `Unavailable`; and no binding
is `NotConfigured`.

## Consequences

- Infrastructure observation cannot silently mutate product configuration.
- Cluster Operators retain control over which existing stack Molejo consumes.
- The Console and developers receive stable availability without provider
  credentials or cluster administration details.
- `molejoctl` remains an explicit runbook and operator utility instead of an
  implicit control plane.
- Adding a provider requires a concrete capability contract and conformance
  evidence, not registration in a universal abstraction.

## Alternatives Considered

Automatically binding the first discovered resource was rejected because
discovery is not consent and ordering is not policy. Keeping bindings only in
the cluster was rejected because the Control Plane could not make stable,
multi-cluster placement decisions. A universal provider table was rejected
because it hides capability-specific semantics, security, and health evidence.

## References

- [ADR 0015: Capability ownership](0015-capability-ownership.md)
- [ADR 0018: Capability observation and feature availability](0018-capability-observation-and-feature-availability.md)
