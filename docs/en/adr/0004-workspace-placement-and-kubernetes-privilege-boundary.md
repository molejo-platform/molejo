# ADR-0004: Workspace placement and Kubernetes privilege boundary

## Status

Proposed for v0.1.0.

## Date

2026-09-10

## Context

Creating a Workspace is a product-governance decision that may also establish a
Kubernetes execution boundary. Dynamic namespace creation requires
cluster-scoped authority, but cluster configuration must not grant product
permissions or combine provisioning, secret delivery, and workload
reconciliation in one credential.

## Decision

Workspace provisioning evaluates four independent gates:

1. **Capability:** the selected cluster supports namespaced provisioning.
2. **Cluster consent:** the Cluster Operator configured provisioning as
   `Disabled` or `Namespaced` through the installation path.
3. **Authorization:** the Control Plane grants the authenticated principal the
   atomic permission required to create the Workspace.
4. **Admission:** product policy validates placement, ownership, limits, and
   idempotency before persisting the operation.

One Workspace placement in one cluster maps to one Molejo-owned namespace. The
Control Plane owns the logical Workspace and placement. `WorkspacePlacement` is
a closed cluster projection containing immutable identity, namespace, a fixed
access profile, and lifecycle state. It cannot carry arbitrary manifests, RBAC
verbs, ServiceAccounts, selectors, or provider configuration.

The Cluster Agent ensures the requested `WorkspacePlacement` projection. A
separate boundary reconciler validates ownership, rejects reserved or foreign
namespaces, and reconciles the namespace and fixed RoleBindings. The runtime
Agent and Platform Operator receive namespaced permissions only for ready
placements. Cluster-wide access remains limited to discovery and Molejo resource
watches that are inherently cluster-scoped.

The Control Plane always revalidates authorization and admission. Capability
observations, cluster consent, and Console presentation never substitute for
those checks.

## Consequences

- A compromised runtime credential has a smaller namespaced blast radius.
- Workspace creation is asynchronous and reports boundary readiness separately.
- The privileged boundary reconciler remains small and does not own application
  workloads, secret values, providers, or the Control Plane connection.
- A namespace is a useful administrative boundary but not hard multi-tenancy
  against hostile workloads, node compromise, or a Kubernetes administrator.

## Alternatives considered

Permanent cluster-wide runtime bindings were rejected because they retain access
outside placed Workspaces. Letting the Agent create arbitrary Namespace or RBAC
objects was rejected because it combines remote execution with privilege
granting. Requiring a local CLI invocation for every Workspace was rejected
because it prevents controlled self-service and automation.

## References

- [ADR-0001: Product boundary and runtime topology](0001-product-boundary-and-runtime-topology.md)
- [ADR-0003: Principals, authentication, and authorization](0003-principals-authentication-and-authorization.md)
- [Security threat model](../architecture/security-threat-model.md)
- [Kubernetes multi-tenancy](https://kubernetes.io/docs/concepts/security/multi-tenancy/)
- [Kubernetes RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
