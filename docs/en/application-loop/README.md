# Application loop

The application loop begins after the foundation and platform are ready. A
developer or CI system registers an immutable image release, changes desired state
through the Molejo API, and observes the resulting operation and application
status. The control plane authorizes and records intent; the outbound Cluster Agent
delivers it; the Platform Operator reconciles the Molejo CRs in Kubernetes.

Build systems, registries, and GitOps engines are composable producers. They do not
patch Molejo-owned Kubernetes resources directly. The first documented integration
is [external CI](external-ci.md).
