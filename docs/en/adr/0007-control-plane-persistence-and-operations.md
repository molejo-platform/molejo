# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Context

Applying product intent to Kubernetes crosses two systems without a distributed
transaction. Retries, duplicate HTTP requests, and API restarts must therefore
be represented explicitly.

## Decision

PostgreSQL is authoritative for identities, Workspace membership, deployment
intent, public IDs, idempotency, sessions, and operation history. Every mutation
persists intent and an operation in one transaction before the runtime effect.

Operations use a lease, worker ID, fencing token, desired version, bounded retry
backoff, and the `Superseded` state. Delete fences later updates. Kubernetes is
authoritative only for observed runtime state; an unavailable or stale runtime
is reported as `Unknown` or `Progressing`, never as current `Ready`.

## Consequences

The executor is recoverable after crashes and duplicate requests are safe. The
database schema is forward-only for this pre-alpha and local data may be
discarded; encrypted manual export is the only documented recovery aid.
