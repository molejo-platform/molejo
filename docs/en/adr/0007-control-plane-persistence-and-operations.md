# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Context

Applying product intent to Kubernetes crosses two systems without a distributed
transaction. Retries, duplicate HTTP requests, and API restarts must therefore
be represented explicitly.

## Decision

PostgreSQL is authoritative for Actor identities, Workspace membership, the
Workspace/Project/App/Environment hierarchy, AppEnvironment configuration,
immutable Deployment history, public IDs, idempotency, sessions, and operation
history. Composite constraints guarantee that an App and Environment bound by
an AppEnvironment belong to the same Project
and Workspace. Relational hierarchy CRUD is synchronous and transactional;
mutations with runtime effects persist intent and an operation in one transaction
before that effect.

Operations use a lease, worker ID, fencing token, desired version, and bounded
retry backoff. Only `EnsureWorkspace`, `ApplyDeployment`, and
`DeleteAppEnvironment` can cross the runtime boundary. Delete fences later updates. Kubernetes is
authoritative only for observed runtime state; an unavailable or stale runtime
is reported as `Unknown` or `Progressing`, never as current `Ready`.

## Consequences

The executor is recoverable after crashes and duplicate requests are safe. The
database schema is forward-only. Because this pre-alpha has no critical
workloads, migration 011 intentionally discards experimental Build, Release,
Deployment, and operation history instead of preserving the removed mutable
model. Encrypted manual export is the only documented recovery aid.
