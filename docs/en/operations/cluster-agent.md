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
managed only through deterministic names derived from Molejo-owned ConfigMaps.

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
renewal attempt returns the same credential; the new certificate and removal of
the renewal intent are persisted atomically. Revoking a Cluster invalidates all
of its credentials and fails its queued or leased operations.

Trust-root replacement is an explicit two-phase operation. Before it, create an
encrypted backup of PostgreSQL and the `molejo-agent-ca`,
`molejo-control-plane-server-ca`, and `molejo-agent-server-tls` Secrets; never
commit those backups. Restore a lost CA instead of replacing it underneath an
active installation.

During transition, each bundle contains the new root first and the old root
second, while the active key already belongs to the new root. The server accepts
client certificates from either root but issues only from the new one. Keep the
old server leaf initially; enrollment and renewal responses distribute both
bundles. Hello advertises a `trustBundleId`. A mismatch forces immediate Agent
renewal, atomic persistence of the certificate, key, and bundles, and
confirmation only after reconnecting with that persisted material.

Switch the server leaf to the new CA only after every `Active` Cluster reports
the target `trustBundleId` through `GET /api/v1/admin/clusters`. Remove old roots
only after that confirmation and the one-hour credential overlap has elapsed;
offline Clusters beyond the operational deadline must be explicitly revoked or
recovered. The ID is derived from the active roots and remains stable when old
trailing roots are removed. An idempotent `molejoctl control-plane install` run
continues to renew only the server leaf while preserving configured roots.

## Runtime protocol and reconciliation

Hello negotiation declares protocol versions and capabilities, establishes the
authoritative session, and reports the clock offset. Commands carry a payload
schema version, desired version, database fencing token, and a deadline bounded
by the operation lease.
The Agent rejects incompatible, malformed, or expired commands. The control
plane accepts a result only while the matching PostgreSQL lease and fencing token
remain authoritative; process memory is not part of correctness.

Every heartbeat can include a complete snapshot of Molejo-owned
`AppDeployment` and `AppVolume` observations. It contains status, desired
version, and a canonical SHA-256 of the `spec`, never open configuration or
Secret values. The control plane reuses durable operations when an object is
missing or any part of its `spec` drifts, including replicas, resources, ports,
probes, exposure, and volumes. The Platform Operator remains responsible for
Kubernetes-level convergence of each CR.
Heartbeats from replaced sessions and repeated or regressing sequences are
rejected before observed state is changed.

Workspace placement is explicit through a Workspace-to-Cluster binding. An
AppEnvironment records its target Cluster, so adding a second Cluster does not
change existing workloads or depend on a global default Agent.
Revocation preserves workloads and history, marks bindings `Failed`, and marks
AppEnvironments `Unknown`. The same Kubernetes UID can enroll under a new
Cluster record only after its previous record is revoked.

## Health and recovery

`/healthz` reports process health. `/readyz` is available in `Unconfigured`,
`Unpaired`, `Enrolling`, `Connecting`, and `Paired`, and unavailable in
`Initializing`, `Stopping`, or `Failed`. `/status` exposes state without identity
material. Network interruption returns to bounded backoff. A certificate renewal
is retried with its persisted attempt; re-enrollment is reserved for a missing,
expired, or administratively revoked identity.
