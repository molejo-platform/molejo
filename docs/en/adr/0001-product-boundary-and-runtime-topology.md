# ADR-0001: Product boundary and runtime topology

## Status

Proposed for v0.1.0.

## Date

2026-09-10

## Context

Molejo exposes an application platform on top of Kubernetes. It must preserve a
portable product contract without turning Kubernetes resources, cluster
administration, or provider infrastructure into its public API. Product intent
also crosses a Control Plane and one or more clusters, so authority and failure
boundaries must remain explicit.

## Decision

The Control Plane owns product identity, authorization, desired state, durable
operations, audit history, and placement. PostgreSQL is the authoritative store
for that state. Kubernetes is authoritative only for observed runtime state.
Neither Kubernetes names nor objects grant product permissions.

The public API accepts Molejo product contracts. It does not accept arbitrary
Kubernetes manifests, resource selectors, API paths, or provider credentials.

Each cluster runs a Cluster Agent with an installation identity. The Agent opens
an outbound mutually authenticated connection to the Control Plane, so no
inbound management endpoint or Control Plane Kubernetes credential is required.
Enrollment, renewal, revocation, session validation, and trust-root transitions
preserve that installation identity without exporting its private key.

The cross-cluster protocol exposes a closed, versioned set of commands, queries,
observations, deadlines, leases, and fencing tokens. Command payload schemas are
validated and versioned; the protocol does not provide a generic Kubernetes
execution channel.

The Agent transports bounded desired state and observations. The Platform
Operator is the only Molejo component that reconciles application CRDs into
native workload, networking, and storage resources. Cluster nodes, networking,
IAM, DNS, Kubernetes upgrades, and provider control planes remain owned by the
Cluster Operator.

## Consequences

- Control Plane restarts and replicas do not make in-memory command state
  authoritative.
- A cluster can be attached without exposing an administrative listener.
- Compromising one component does not automatically grant the privileges of the
  others, although each component remains a trust boundary.
- Multi-cluster placement can evolve without changing the application-facing
  resource model.
- Cryptographic algorithms, certificate lifetimes, retry intervals, and concrete
  Kubernetes resources remain security-profile or contract details rather than
  permanent ADR content.

## Alternatives considered

Making Kubernetes the product API was rejected because it couples users to the
execution substrate and bypasses product authorization. Giving the public API a
cluster credential was rejected because it broadens its blast radius. An inbound
Agent callback was rejected because it requires management exposure in every
cluster. Allowing the Agent to render arbitrary native resources was rejected
because it creates a remote Kubernetes administration channel.

## References

- [Operational model](../architecture/operational-model.md)
- [Cluster Agent operations](../platform/cluster-agent.md)
- [Platform Operator](../platform/platform-operator.md)
- [Security threat model](../architecture/security-threat-model.md)
