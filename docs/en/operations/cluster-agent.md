# Cluster Agent Operations

The Cluster Agent is the only Molejo component that executes control-plane
runtime intent against Kubernetes. It initiates an outbound TLS 1.3 mTLS stream;
the public API never receives a kubeconfig and the cluster exposes no inbound
management port.

## Installation and trust

Install the Operator and Agent with `molejoctl cluster install`, then run
`molejoctl control-plane install`. A new installation creates separate ECDSA
P-256 trust roots:

- `molejo-agent-ca` signs Agent client identities;
- `molejo-control-plane-server-ca` signs the internal API/gRPC server identity.

The split prevents a stolen server-signing key from minting Agent identities.
Existing alpha installations that still use one CA keep that trust layout until
their chart is upgraded, so an idempotent installer run does not break the
Console or Agent connection.

The Agent ServiceAccount has narrowly scoped access to its two named identity
Secrets and to the Kubernetes resources required by the runtime contract. It
cannot list arbitrary Secrets. Runtime configuration Secrets are selected and
managed only through Molejo-owned objects.

## Cluster identity and enrollment

A Cluster is a durable control-plane record. Its credentials are rotating child
records, not the identity of the Cluster itself. An installation administrator
creates a Cluster with `POST /api/v1/admin/clusters` and transfers the returned
ten-minute, single-use token to `molejo-agent-enrollment` without placing it in
Git, shell history, logs, or chat.

The Agent persists its key, CSR, and attempt ID before enrollment. Repeating the
same attempt and CSR is idempotent. The private key never leaves the cluster.
After validating the signed certificate, URI SAN, trust roots, and expiry, the
Agent removes the enrollment token and opens its outbound stream.

Client certificates last seven days and are renewed automatically during the
last 24 hours. Renewal creates and persists a new key and CSR before making the
authenticated request. The previous credential remains valid for a one-hour
overlap so an interrupted rotation can converge safely. Replaying the same
renewal attempt returns the same credential. Revoking a Cluster invalidates all
of its credentials and fails its queued or leased operations.

Trust-root replacement is not an automatic repair. Back up both CA Secrets. If
the server CA is lost, the installer stops and requires restoration rather than
silently replacing trust and disconnecting already paired Agents.
An idempotent `molejoctl control-plane install` run renews the internal server
leaf certificate when less than 30 days remain under the same trusted CA.

## Runtime protocol and reconciliation

Hello negotiation declares protocol versions and capabilities. Commands carry a
payload schema version, desired version, database fencing token, and deadline.
The Agent rejects incompatible, malformed, or expired commands. The control
plane accepts a result only while the matching PostgreSQL lease and fencing token
remain authoritative; process memory is not part of correctness.

Every heartbeat can include a complete snapshot of Molejo-owned
`AppDeployment` and `AppVolume` observations. It contains status and identifiers,
never configuration or Secret values. The control plane stores observations and
reuses the durable `ApplyDeployment` operation when an object is missing or its
observed image differs from desired state. The Platform Operator remains
responsible for Kubernetes-level convergence of each CR.

Workspace placement is explicit through a Workspace-to-Cluster binding. An
AppEnvironment records its target Cluster, so adding a second Cluster does not
change existing workloads or depend on a global default Agent.

## Health and recovery

`/healthz` reports process health. `/readyz` is available in `Unconfigured`,
`Unpaired`, `Enrolling`, `Connecting`, and `Paired`, and unavailable in
`Initializing`, `Stopping`, or `Failed`. `/status` exposes state without identity
material. Network interruption returns to bounded backoff. A certificate renewal
is retried with its persisted attempt; re-enrollment is reserved for a missing,
expired, or administratively revoked identity.
