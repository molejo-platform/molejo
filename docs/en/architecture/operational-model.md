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

The current release is alpha. Contracts and command paths may break between alpha
releases. The supported transition is a clean experimental reinstall, not an
in-place upgrade. Ownership checks still prevent Molejo from adopting or
overwriting foreign resources.
