# ADR-0002: Product resource model and application delivery loop

## Status

Proposed for v0.1.0.

## Date

2026-09-10

## Context

Molejo must preserve one product model across Console workflows, external CI,
managed builders, the Control Plane, and Kubernetes reconciliation. Treating an
image tag, a build, a release, and a deployment as the same resource would make
history mutable and couple delivery to one producer.

## Decision

The product hierarchy is:

```text
Workspace
└── Project
    ├── App
    └── Environment
         ↘ AppEnvironment ↙
```

An `App` is a logical application identity. An `Environment` is a deployment
stage inside the same Project. An `AppEnvironment` binds exactly one App to one
Environment and owns its desired runtime configuration and placement.

A `Release` records an immutable OCI artifact using a digest-pinned image
reference. Builds, external CI systems, and future producers may create the same
Release resource, but producer metadata does not change its lifecycle. Declared
provenance remains distinguishable from independently attested provenance.

A `Deployment` is a separate immutable record that selects a Release and a
specific AppEnvironment configuration revision. Creating it persists desired
state and a durable, idempotent operation before any cluster effect. The Control
Plane dispatches that intent through the bounded runtime channel and reconciles
the result into product state.

External automation authenticates as a non-human principal with explicit App and
AppEnvironment permissions. It registers Releases and requests Deployments
through the Control Plane API; it does not patch Molejo-managed Kubernetes
resources directly.

## Consequences

- The same Release identifies the same artifact regardless of its producer.
- Release history and deployment history remain auditable and immutable.
- CI, registries, managed builders, and future GitOps adapters compose around the
  same product lifecycle.
- Runtime retries can be fenced without creating duplicate product intent.
- Build orchestration, source providers, rollout strategies, and registry login
  remain separate capabilities.

## Alternatives considered

Using mutable image tags as Releases was rejected because the referenced bytes
can change. Treating a successful build as an implicit deployment was rejected
because promotion policy belongs to the AppEnvironment. Direct Kubernetes
mutation by CI was rejected because it bypasses authorization, audit, desired
state, and reconciliation.

## References

- [Application loop](../application-loop/README.md)
- [External CI releases](../application-loop/external-ci.md)
- [ADR-0001: Product boundary and runtime topology](0001-product-boundary-and-runtime-topology.md)
