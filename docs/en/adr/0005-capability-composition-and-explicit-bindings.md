# ADR-0005: Capability composition and explicit bindings

## Status

Proposed for v0.1.0.

## Date

2026-09-10

## Context

Molejo must explain whether an application workflow is usable while composing
with infrastructure selected by a Cluster Operator. Protocol support, observed
cluster facts, provider configuration, consent, and developer-facing
availability have different owners and lifecycles.

## Decision

Molejo separates four contracts:

1. A **Protocol Capability** negotiates optional behavior between versioned
   Molejo components. It does not prove provider or cluster health.
2. A **Capability Observation** is a bounded, time-stamped fact reported by an
   authenticated Cluster Agent. It cannot mutate desired state.
3. A **Provider Binding** is explicit, typed Control Plane configuration that
   connects a product concern to one concrete implementation.
4. **Feature Availability** is a read-only projection derived from product state,
   protocol support, fresh observations, cluster consent, and bindings.

Capability identifiers describe provider-neutral outcomes. Provider names and
Kubernetes distributions are adapter facts or binding kinds, not feature IDs.
Availability remains structural and never embeds actor authorization.

Discovery may present candidates but never activates a binding. A Cluster
Operator creates, changes, or removes a binding through an authenticated flow.
Bindings are narrow contracts for concrete capabilities; Molejo does not expose
a universal provider registry or arbitrary provider JSON.

Infrastructure ownership is explicit: external, provider-managed,
runbook-managed, or Molejo-managed. A `molejoctl` runbook may plan, apply, verify,
and remove resources it owns, but it does not transfer third-party lifecycle
ownership to the Control Plane, Agent, or Operator.

Current and historical telemetry are separate capabilities. Current data can be
queried from bounded cluster APIs and is ephemeral. Historical data requires an
explicit provider binding with its own retention, isolation, authentication, and
conformance guarantees. Public product APIs expose normalized product concepts,
not provider query languages or credentials.

## Consequences

- Operators can retain Kubernetes, cloud, registry, storage, publication, and
  telemetry stacks they already operate.
- The Console can explain unavailable workflows without attempting hidden
  infrastructure changes.
- One cluster cannot satisfy another cluster's capability requirements.
- Provider adapters evolve behind capability-specific ports without leaking
  vendor syntax into application contracts.
- Prometheus-compatible metrics, OpenBao, GitHub, and other initial adapters are
  implementation guides rather than permanent product decisions.

## Alternatives considered

Automatically binding the first discovered provider was rejected because
discovery is not consent. A generic plugin registry was rejected because it
hides capability-specific security and failure semantics. Provider names as
feature identifiers were rejected because they couple application UX to an
installation choice. Falling back from historical to current telemetry was
rejected because their retention and completeness guarantees differ.

## References

- [Operational model](../architecture/operational-model.md)
- [Cluster capabilities](../capabilities/README.md)
- [ADR-0001: Product boundary and runtime topology](0001-product-boundary-and-runtime-topology.md)
