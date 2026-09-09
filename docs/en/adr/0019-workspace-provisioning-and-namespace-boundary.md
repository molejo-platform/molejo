# ADR 0019: Workspace provisioning and namespace boundary

## Status

Accepted for the alpha architecture.

## Context

Creating a Workspace is both a product-governance decision and a request to
establish a Kubernetes execution boundary. Treating a cluster observation, a
Console feature flag, or possession of Cluster Agent credentials as
authorization would allow infrastructure configuration to escalate product
privileges.

Dynamic namespace creation also requires a small amount of cluster-scoped
authority. That authority cannot be eliminated while Workspace creation remains
self-service, but it can be isolated from application reconciliation, secret
values, and public traffic.

## Decision

Every Workspace provisioning request passes four independent gates:

1. **Capability**: the selected cluster reports that namespaced Workspace
   provisioning is supported and healthy.
2. **Cluster consent**: the Cluster Operator configures the Agent with the
   explicit mode `Disabled` or `Namespaced`. This setting is changed through the
   cluster installation path, not the Control Plane API.
3. **Authorization**: the Control Plane verifies an atomic permission such as
   `installation.workspace.create` for the authenticated actor.
4. **Admission**: pure policy validates the selected cluster, namespace,
   Workspace class, ownership assignment, limits, and idempotency before any
   operation is accepted.

Feature Availability may describe the first two gates. It never grants the
third or bypasses the fourth. The API always enforces authorization and admission
even when the Console has already evaluated the same facts.

For the alpha, only an Installation Administrator may create a Workspace. A
future non-human Workspace Provisioner may receive the same atomic permission
with explicit cluster, class, owner-assignment, rate, and lifetime constraints.
Human and automated entry points must call the same idempotent application use
case; no integration may call the Cluster Agent directly.

One Workspace placement in one cluster maps to one Molejo-owned namespace. The
Control Plane owns the logical Workspace and placement. A cluster-scoped
`WorkspacePlacement` custom resource is its bounded runtime projection and
contains only immutable product identity, namespace name, a fixed access-profile
identifier, and desired lifecycle state. It cannot carry arbitrary manifests,
RBAC verbs, role references, ServiceAccounts, selectors, or provider
configuration.

The Cluster Agent may reconcile `WorkspacePlacement` resources but does not
create Namespaces or RBAC resources directly. A Workspace boundary reconciler
creates or adopts only namespaces already carrying compatible Molejo ownership,
rejects reserved or foreign namespaces, and binds fixed Agent and runtime
Operator ClusterRoles through RoleBindings in that namespace.

Boundary provisioning and application reconciliation are separate privilege
domains. They may share a binary artifact, but they run as separate workloads
and ServiceAccounts. The boundary reconciler has no permission to read Secrets,
create application workloads, or connect to the Control Plane. Agent and runtime
Operator namespaced permissions are granted only after the placement is ready.
Only discovery and fixed Molejo CR watches that are inherently cluster-scoped
remain behind ClusterRoleBindings.

## Consequences

- A cluster configuration signal cannot grant a user or automation product
  privileges.
- A compromised Agent token is limited to explicitly bound Workspace
  namespaces for namespaced resources.
- Workspace creation becomes asynchronous and reports conditions such as
  `NamespaceReady`, `AgentAccessReady`, `OperatorAccessReady`, and
  `PolicyReady`.
- Native Kubernetes RBAC cannot constrain every field of a RoleBinding created
  by a privileged controller. The boundary reconciler remains a high-trust
  component and must have a small attack surface; admission enforcement can
  further constrain it when a portable policy is proven.
- Namespace isolation reduces blast radius but is not a hard security boundary
  against hostile workloads sharing a cluster or node.

## Alternatives Considered

Keeping cluster-wide Agent and Operator bindings was rejected because a stolen
token would retain access outside Molejo Workspaces.

Letting the Agent create Namespace and RoleBinding objects directly was rejected
because it combines remote command execution, secret delivery, and privilege
granting in one credential.

Making the Console feature flag authoritative was rejected because client-side
state is not an authorization control.

Requiring `molejoctl` to create every Workspace namespace was rejected because
it prevents the accepted administrative and future automated self-service
journeys.

## References

- [ADR 0015: Capability ownership](0015-capability-ownership.md)
- [ADR 0018: Capability observation and feature availability](0018-capability-observation-and-feature-availability.md)
- [Molejo security threat model](../architecture/security-threat-model.md)
- [Kubernetes RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
- [Kubernetes multi-tenancy](https://kubernetes.io/docs/concepts/security/multi-tenancy/)
