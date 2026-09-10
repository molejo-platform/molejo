# ADR 0020: Secret custody and runtime delivery

## Status

Accepted for the alpha architecture.

## Context

An application secret crosses the public API, the Control Plane, a secret
backend, the outbound Agent channel, the Kubernetes API, and the application
process. Treating Kubernetes Secret, PostgreSQL, or one vendor product as the
universal secret architecture would either increase exposure or prevent future
delivery modes from being composed safely.

Secret storage and secret delivery have different trust boundaries. The same
external backend can be resolved by the Control Plane and materialized in
Kubernetes, or consumed by a cluster-side mechanism without revealing the value
to the Control Plane. Those are different security contracts and must not be one
provider switch.

## Decision

The source of truth for secret values is an external `SecretValueStore`. The
Control Plane database stores an opaque internal handle, backend version,
fingerprint, lifecycle metadata, and audit events; it never stores the plaintext
value or exposes backend paths as public IDs. OpenBao is the initial adapter, not
a product requirement. Future adapters may include AWS Secrets Manager, SSM
SecureString, Google Secret Manager, or another backend only after their concrete
versioning and failure semantics are modeled.

The public parameter API is write-only for values. Reads return metadata and
state, never plaintext. Logs, metrics, traces, events, error messages,
idempotency records, observations, snapshots, and test fixtures must not contain
secret values.

Secret delivery is a separate contract. The alpha uses
`MaterializedKubernetesSecret`:

1. the Control Plane resolves only the values required for one accepted
   deployment operation;
2. the value travels over the authenticated mTLS Agent channel in a bounded,
   typed payload;
3. the Agent creates an immutable, versioned, application-environment-specific
   Kubernetes Secret;
4. the Platform Operator receives only the Secret reference and projects it
   into the application workload;
5. rotation creates a new version and updates desired state; an old version is
   removed only after it is no longer referenced by the reconciled workload.

The Platform Operator never receives secret values and has no explicit Secret
read permission. Application workloads do not receive a Kubernetes ServiceAccount
token by default and cannot select arbitrary Secret references through the
Molejo application contract.

The Agent receives `get`, `create`, and `delete` for Secrets only through a
RoleBinding in each ready Workspace namespace. It does not receive `list` or
`watch`. `get` is retained because immutable object ownership and idempotency
must be verified. Provider credentials, TLS private keys, registry credentials,
and Molejo component credentials stay in dedicated system or capability
namespaces and never share a Workspace namespace.

Kubernetes Secret is a last-mile materialization, not a durable vault or
fallback source of truth. PostgreSQL plaintext and in-process durable caches are
also forbidden fallbacks. Cluster encryption at rest, KMS configuration, etcd
operations, and node hardening remain Cluster Operator responsibilities;
Molejo may observe and diagnose their absence without silently enabling them.

Future delivery modes such as External Secrets Operator, Secrets Store CSI, or
application workload identity are added behind a delivery contract independent
of `SecretValueStore`. A mode that avoids Control Plane plaintext is a distinct
security profile and must declare its own availability, custody, rotation, and
failure guarantees.

## Consequences

- Secret backends remain replaceable without changing the parameter or
  application contracts.
- Kubernetes receives the minimum value needed to start the workload, but any
  application that receives a secret can still disclose its own value.
- The Control Plane and external secret backend remain high-value trust
  boundaries in the alpha materialization mode.
- Rotation is explicit and versioned instead of mutating shared Secret objects
  in place.
- Namespace-scoped Agent RBAC limits a stolen token to the Workspaces explicitly
  bound to that Agent credential.
- Direct provider delivery can reduce Control Plane custody later without
  forcing a rewrite of the storage port.

## Alternatives Considered

Using Kubernetes Secrets as the durable source of truth was rejected because it
couples product state to one cluster, broadens Kubernetes read requirements, and
does not provide a portable multi-cluster custody model.

Storing encrypted values in PostgreSQL was rejected as the default because key
custody would still need an external root of trust and would duplicate a secret
manager lifecycle inside the Control Plane.

Requiring External Secrets Operator or CSI for the alpha was rejected because it
would make the minimum application loop depend on an optional cluster stack.

Combining backend and delivery in a universal provider interface was rejected
because it hides materially different custody and failure boundaries.

## References

- [ADR 0015: Capability ownership](0015-capability-ownership.md)
- [ADR 0019: Workspace provisioning and namespace boundary](0019-workspace-provisioning-and-namespace-boundary.md)
- [Molejo security threat model](../architecture/security-threat-model.md)
- [Good practices for Kubernetes Secrets](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
- [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
