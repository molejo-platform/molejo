# ADR-0011: Portable Stateful Runtime and Volume Lifecycle

Status: Draft

## Context

`AppEnvironment` already owns the branch and runtime configuration for one App
inside one Environment. Some applications also require data that survives a new
release or workload recreation. Exposing PVCs, StorageClasses, CSI drivers,
nodes, zones, or provider identifiers would turn Kubernetes infrastructure into
the public product API and would make the same workflow differ across clusters.

## Decision

`AppEnvironment.workloadKind` is required at creation and is either `Stateless`
or `Stateful`. It is not changed by the ordinary update contract. The choice
belongs to App plus Environment, so the same App may be Stateless in one
Environment and Stateful in another. Every immutable Deployment snapshot records
the selected kind.

The internal `AppDeployment` remains the stable runtime intent and uses a
discriminated workload union. Stateless creates a Deployment and rejects
persistent attachments. Stateful creates a StatefulSet, requires exactly one
replica and one ReadWriteOnce attachment, and preserves the existing Service,
HTTPRoute, probes, resources, configuration, security, and observability
contracts. Its root filesystem remains read-only; only the declared mount is
writable.

`AppVolume` is a separate namespaced intent owned by the AppEnvironment. It maps
to one durable PersistentVolumeClaim but has no owner reference to a release or
workload. Releases and StatefulSets reference it and cannot delete it. Expansion
is upward-only. Deletion is a distinct, idempotent, audited operation protected
by optimistic concurrency and rejected while an active Deployment references the
volume.

Users select a product `StorageProfile`. Public capabilities contain only name,
size limits, available quota, expansion, snapshot, backup, and durability
semantics. The installation privately binds that profile to a StorageClass. The
initial profile is `persistent-standard`; changing its binding between local
storage, EBS CSI, or DigitalOcean Block Storage does not change the domain, HTTP
contract, or Console workflow.

PostgreSQL is authoritative for intent, versions, quota reservation, operations,
and audit metadata. Kubernetes is authoritative only for observed runtime state.
Public states and reasons are sanitized. Internal claim names, UIDs, storage
classes, CSI handles, topology, and raw Kubernetes errors are never returned.

## Consequences

The product gains an explicit stateful path without duplicating its delivery and
observability model or coupling it to one provider. The first increment is
pre-alpha and intentionally supports one replica, one volume, ReadWriteOnce,
preserve-by-default retention, and upward expansion only.

This decision does not provide conversion between workload kinds, shared or
multi-attach volumes, snapshots, backup, restore, import, cloning, multi-zone
availability, failover, or production guarantees. Node-local profiles must be
presented as node-local durability rather than high availability.

## References

- [ADR-0002: Reconciliation State and Observability Contract](0002-reconciliation-state-and-observability-contract.md)
- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [ADR-0008: Exact Source Builds and Immutable Releases](0008-exact-source-builds-and-immutable-releases.md)
- [ADR-0010: Immutable Platform Configuration Releases](0010-immutable-platform-configuration-releases.md)
