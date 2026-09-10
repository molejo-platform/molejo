# Operational model

Molejo manages applications on Kubernetes; it does not provision clusters or own
their nodes, network, IAM, DNS, or provider control plane. The product is divided
into four operational areas:

1. **Foundation** observes the Kubernetes substrate and its prerequisites without
   mutating them.
2. **Platform lifecycle** installs and diagnoses Molejo-owned components.
3. **Capabilities** contains explicit, inspectable runbooks that help an operator
   connect standard Kubernetes or provider services to Molejo.
4. **Application loop** covers the developer path from an immutable release to
   desired application state and runtime status.

This boundary is also the CLI vocabulary: `foundation`, `platform`, and
`capability`. Application-loop operations stay in the product API and Console;
the CLI will only expose them when a concrete operator workflow requires it.

Molejo contracts describe product intent. Capability recipes are local input
documents, not product CRDs or a second source of truth. A recipe must expose its
plan, ownership, inputs, verification, and teardown implications. It may use a
provider tool or credential selected by the operator, but it must not hide cluster
creation or node administration.

## Security decision layers

Structural availability, Cluster Operator consent, actor authorization, and
request admission are independent decisions. A cluster observation or a Console
feature flag can explain whether a workflow is technically available; neither
can grant a product permission. Every mutation is authorized and admitted again
by the Control Plane.

The target contract lets the Cluster Operator enable `Disabled` or `Namespaced`
Workspace provisioning through the Agent installation. In `Namespaced` mode, one
Workspace placement in one cluster maps to one Molejo-owned namespace. The
Control Plane owns the logical Workspace, the Cluster Agent transports bounded
desired state, and a separate boundary privilege creates the namespace and fixed
namespaced RoleBindings.

Secret storage and runtime delivery are also separate concerns. When secret
parameters are enabled, an external SecretValueStore is the durable source of
truth. A versioned Kubernetes Secret is the initial last-mile delivery mechanism,
not a vault or fallback. See ADR 0006 and the security threat model.

The current release is alpha. Contracts and command paths may break between alpha
releases. The supported transition is a clean experimental reinstall, not an
in-place upgrade. Ownership checks still prevent Molejo from adopting or
overwriting foreign resources.

Runtime facts collected by the Cluster Agent are Capability Observations, not
Foundation state. The Control Plane combines fresh observations, protocol
negotiation, product state, and typed provider bindings into a read-only Feature
Availability projection. This projection never mutates desired application
state.

Current telemetry and historical telemetry are separate capabilities. Current
cluster data is bounded and ephemeral; historical data depends on a configured
provider and its retention guarantees. One is never presented as a transparent
fallback for the other.

## Security references

- [Product boundary and runtime topology](../adr/0001-product-boundary-and-runtime-topology.md)
- [Workspace placement and Kubernetes privilege boundary](../adr/0004-workspace-placement-and-kubernetes-privilege-boundary.md)
- [Capability composition and explicit bindings](../adr/0005-capability-composition-and-explicit-bindings.md)
- [Secret custody and runtime delivery](../adr/0006-secret-custody-and-runtime-delivery.md)
- [Security threat model](security-threat-model.md)
