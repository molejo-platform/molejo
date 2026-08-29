# ADR-0010: Immutable Platform Configuration Releases

Status: Draft

## Context

Platform processes read configuration and credentials at startup. Updating a
fixed-name ConfigMap or Secret does not change a Pod template, and imperative
restarts make unchanged applies non-idempotent and obscure the active version.

## Decision

Non-secret platform configuration is rendered into immutable, content-addressed
ConfigMaps per consumer. Rotatable credentials remain outside Git and are copied
to immutable Secret versions; rendered releases reference exact names. Managed
certificates may project a non-secret certificate fingerprint into the Pod
template when their controller requires a stable Secret name.

Each release is rendered from a clean committed checkout and records its source
commit. An unchanged release leaves Pod templates unchanged. Garbage collection
runs only after acceptance, retains the two newest versions, and never deletes a
version still referenced by a Deployment, StatefulSet, DaemonSet, or Job.

## Consequences

Configuration changes roll only their consumers, rollback can select an earlier
version, and credential rotation cannot silently split consumers between old and
new values. Secret material remains absent from Git and release metadata. The lab
still uses single replicas and does not claim uninterrupted rotation or HA.
