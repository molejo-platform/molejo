# ADR-0013: Outbound Cluster Agent Identity and Pairing

Status: Accepted

## Context

The Operator can reconcile runtime intent without being exposed to a remote
control plane. A portable installation still needs a narrow connection that can
cross cluster and network boundaries without giving the public API a Kubernetes
credential or requiring an inbound management port on the cluster.

## Decision

The Cluster Agent owns one installation identity and initiates an outbound gRPC
stream to the control plane. It generates an ECDSA P-256 key in the cluster,
persists it in one named Secret, enrolls through a ten-minute one-time token, and
sends only a CSR. An installer-provided Agent CA signs a seven-day client
certificate whose URI SAN is `spiffe://molejo.dev/agent/{installationId}`.

The gRPC connection requires TLS 1.3 and mutual authentication. Durable Cluster
identity and rotating credentials are separate records. The declared Cluster,
certificate URI, certificate fingerprint, and PostgreSQL record must agree.
Client and server identities use separate trust roots on new installations.
Seven-day client certificates renew automatically with a persisted idempotency
attempt and one-hour credential overlap. Administrators can revoke a Cluster and
all credentials derived from it.

The versioned protocol negotiates capabilities and carries commands with a
schema version, deadline, durable lease, and fencing token. The Agent reports
only non-sensitive observations of Molejo-owned runtime objects. Desired state,
routing, audit, and operation lifecycle remain authoritative in PostgreSQL.
Workspace-to-Cluster bindings and AppEnvironment placement are explicit.

## Consequences

The cluster retains its private key and accepts no inbound management traffic.
The control plane receives an authenticated, versioned transport without
becoming the owner of Kubernetes credentials. Pairing is installation-scoped and
is not implied by a Workspace membership.

The control plane can restart or run multiple API replicas without losing result
fencing because active command state is not process-local. An Agent may reconnect
with the superseded credential during the bounded rotation overlap, but at most
one live operation is leased to a Cluster. Adding a Cluster does not implicitly
move workloads. Kubernetes reconciliation remains the Platform Operator's
responsibility; the Agent only applies contracted intent and reports observation.

## Alternatives Considered

Embedding the connector in the public API couples Kubernetes credentials to the
control plane deployment. Inbound callbacks require cluster exposure. Persisting
the private key in PostgreSQL transfers ownership away from the cluster. These
alternatives were not selected.

## References

- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Cluster Agent operations](../operations/cluster-agent.md)
