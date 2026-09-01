# ADR-0013: Outbound Cluster Agent Identity and Pairing

Status: Draft

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

The gRPC connection requires TLS 1.3 and mutual authentication. The declared
installation, certificate URI, certificate fingerprint, and PostgreSQL record
must agree. The first protocol version exchanges only hello and heartbeat
messages. The Agent has no AppDeployment or general Secret permission and may
start healthy before the control plane or enrollment token exists.

## Consequences

The cluster retains its private key and accepts no inbound management traffic.
The control plane receives an authenticated, versioned transport without
becoming the owner of Kubernetes credentials. Pairing is installation-scoped and
is not implied by a Workspace membership.

This pre-alpha increment has one Agent replica and no commands, queue, local
database, leader election, automatic certificate rotation, revocation API, CLI,
or Console flow. Certificates expire after seven days, so rotation must be
implemented before this boundary is considered operationally durable.

## Alternatives Considered

Embedding the connector in the public API couples Kubernetes credentials to the
control plane deployment. Inbound callbacks require cluster exposure. Persisting
the private key in PostgreSQL transfers ownership away from the cluster. These
alternatives were not selected.

## References

- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Cluster Agent operations](../operations/cluster-agent.md)
