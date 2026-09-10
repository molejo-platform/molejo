# ADR 0018: Capability observation and feature availability

## Status

Accepted for the alpha architecture.

## Context

Molejo must explain which application workflows are usable without treating a
Kubernetes distribution, cloud product, or third-party stack as a product
feature. Protocol negotiation, cluster facts, provider configuration, and the
developer-facing result have different owners and lifecycles.

## Decision

Molejo uses four separate contracts:

1. A **Protocol Capability** is a versioned string exchanged by Molejo binaries
   to negotiate optional wire behavior. It does not prove that an underlying
   cluster or provider is usable.
2. A **Capability Observation** is a bounded, time-stamped fact collected by the
   authenticated Cluster Agent. It describes support and health without
   changing desired application state.
3. A **Provider Binding** is a typed Control Plane configuration that connects a
   product concern to one external implementation. It is not a generic plugin
   registry and it never exposes credentials or provider endpoints to clients.
4. **Feature Availability** is a read-only projection derived by the Control
   Plane from product state, fresh observations, protocol negotiation, and
   provider bindings.

Capability identifiers express provider-neutral outcomes such as
`runtime.workload.apply`, `runtime.logs.current`, or
`telemetry.metrics.historical`. Provider and Kubernetes distribution names are
facts or binding kinds, never feature identifiers.

Availability is scoped to a Workspace, App, or AppEnvironment and has one of
these states: `Available`, `Limited`, `NotConfigured`, `Unavailable`,
`Unsupported`, or `Unknown`. Stable reason codes explain the result. Human
authorization remains a separate decision and is never encoded as structural
availability.

The Control Plane receipt time is authoritative for observation freshness. An
expired observation or disconnected Agent resolves to `Unknown`; it does not
remain falsely available and it is not reclassified as unsupported. A complete
snapshot atomically replaces the previous snapshot. A partial snapshot only
updates the capabilities it contains.

Current telemetry and historical telemetry are different capabilities. Current
data may come directly from Kubernetes and is bounded and ephemeral. Historical
data requires a configured provider with its own retention and query guarantees.
Neither silently substitutes for the other.

During alpha, protocol and persistence additions should remain compatible with
older Agents when that is inexpensive. Clean reinstall remains the only
supported transition between alpha releases, as defined by ADR 0016.

## Ownership

- The Cluster Agent observes cluster facts and reports them over authenticated
  transport.
- The Control Plane owns bindings, freshness, resolution, and the public read
  model.
- The Platform Operator remains responsible only for reconciling Molejo runtime
  resources.
- `molejoctl` may inspect the same vocabulary locally, but local evidence cannot
  overwrite Control Plane observations.
- The Console consumes availability and combines it with actor authorization at
  presentation time.

## Consequences

The Console can explain unavailable workflows before a provider call fails, one
cluster cannot satisfy another cluster's requirements, and optional providers
remain composable. Observations never install infrastructure, rotate provider
credentials, or mutate desired application state.

## References

- [ADR 0015: Capability ownership](0015-capability-ownership.md)
- [ADR 0016: Alpha lifecycle policy](0016-alpha-lifecycle-policy.md)
- [ADR 0021: Explicit operator-managed bindings](0021-explicit-operator-managed-bindings.md)
- [ADR 0022: Provider-neutral metrics with a Prometheus-compatible query adapter](0022-provider-neutral-metrics-with-prometheus-query.md)
