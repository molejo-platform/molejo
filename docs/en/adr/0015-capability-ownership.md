# ADR 0015: Capability ownership

## Status

Accepted for the alpha architecture.

## Decision

Molejo classifies infrastructure integrations as `external`, `runbook-managed`,
`molejo-managed`, or `provider-managed`. `molejoctl capability` may automate an
explicit runbook, but the Platform Operator, Cluster Agent, and control plane do
not become lifecycle owners of third-party infrastructure.

Runbooks use local versioned input, show a plan before mutation, label only their
owned resources, refuse unsafe adoption, and provide read-only verification. Their
documents are not CRDs and are not persisted as product state.

Authenticated Cluster Agent observations are runtime facts, not Foundation
state or capability runbooks. The Control Plane may combine fresh observations
with typed provider bindings to derive Feature Availability, as defined by ADR
0018, but that projection does not transfer lifecycle ownership to Molejo.

Cluster Operator consent to namespaced Workspace provisioning is installation
configuration, not actor authorization. Secret backend custody and runtime
delivery remain separate typed contracts, as defined by ADRs 0019 and 0020.

## Consequences

Operators can compose EKS, GKE, K3s, cloud-managed services, or OSS components
without changing the application contract. Molejo may offer more runbooks later,
but each integration keeps a visible owner and lifecycle boundary.

## References

- [ADR 0018: Capability observation and feature availability](0018-capability-observation-and-feature-availability.md)
- [ADR 0019: Workspace provisioning and namespace boundary](0019-workspace-provisioning-and-namespace-boundary.md)
- [ADR 0020: Secret custody and runtime delivery](0020-secret-custody-and-runtime-delivery.md)
