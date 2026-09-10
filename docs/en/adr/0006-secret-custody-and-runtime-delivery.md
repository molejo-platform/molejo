# ADR-0006: Secret custody and runtime delivery

## Status

Proposed for v0.1.0.

## Date

2026-09-10

## Context

An application secret crosses the public API, the Control Plane, a secret store,
the outbound Agent channel, Kubernetes, and the application process. Storage and
last-mile delivery have different trust, availability, rotation, and failure
boundaries.

## Decision

Secret parameters are an optional capability. When enabled, their durable source
of truth is an external `SecretValueStore`. The Control Plane database stores an
opaque internal handle, backend version, fingerprint, lifecycle metadata, and
audit history; it does not store plaintext or expose backend paths as public IDs.

The public API is write-only for secret values. Reads return metadata and state,
never plaintext. Secret values must not enter logs, metrics, traces, events,
errors, idempotency records, observations, or fixtures.

Secret storage and delivery are separate ports. The initial delivery mode
materializes an immutable, versioned Kubernetes Secret for one AppEnvironment:

1. the Control Plane resolves only values required by an accepted operation;
2. values travel in the bounded, versioned runtime command over the mutually
   authenticated Agent channel;
3. the Agent creates or verifies the immutable Secret in the placed namespace;
4. the Platform Operator receives only a Secret reference and projects it into
   the workload;
5. an old version is removed only after the reconciled workload no longer
   references it.

The Platform Operator does not read secret values. The Agent receives only the
namespaced verbs required for idempotent materialization and collection; it does
not list or watch Secrets. Application workloads do not receive a Kubernetes
ServiceAccount token by default and cannot select arbitrary Secret references.

Kubernetes Secret is a last-mile materialization, not a durable vault or
fallback. Cluster encryption at rest, KMS, etcd, node hardening, and provider
credentials remain Cluster Operator responsibilities. A future delivery mode
must declare its custody and failure boundaries independently of the storage
adapter.

## Consequences

- Secret backends can change without changing application parameter contracts.
- The Control Plane sees plaintext transiently in the initial delivery mode and
  remains a high-value trust boundary.
- Rotation creates a new version rather than mutating a shared Secret in place.
- A workload can still disclose a secret intentionally delivered to it.
- An unavailable external store makes secret parameters unavailable without
  making the core application loop unhealthy.

## Alternatives considered

Kubernetes Secrets as the durable source were rejected because they couple
custody to one cluster. PostgreSQL plaintext or an in-process durable cache were
rejected because they broaden exposure. Requiring one cluster-side secret system
was rejected because it turns an optional stack into a core dependency.
Combining storage and delivery behind one universal provider interface was
rejected because it hides materially different trust boundaries.

## References

- [ADR-0004: Workspace placement and Kubernetes privilege boundary](0004-workspace-placement-and-kubernetes-privilege-boundary.md)
- [ADR-0005: Capability composition and explicit bindings](0005-capability-composition-and-explicit-bindings.md)
- [Security threat model](../architecture/security-threat-model.md)
- [Kubernetes Secrets good practices](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
- [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
