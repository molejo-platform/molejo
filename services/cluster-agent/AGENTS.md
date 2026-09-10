# Cluster Agent development

The Cluster Agent is the outbound, authenticated bridge between one Kubernetes
cluster and the Control Plane. It transports bounded commands and observations;
it does not own product authorization or desired state.

## Explore

- Start at `cmd/cluster-agent/` for configuration and process lifecycle.
- Use `internal/agent/` for orchestration, `internal/controlplane/` for the remote
  channel, `internal/runtime/` for commands and queries, and `internal/capability/`
  for observations.
- Identity and Kubernetes effects stay in `internal/identity/` and `internal/kube/`.
- The wire contract is [`../../contracts/molejo/clusteragent/v1alpha1/agent.proto`](../../contracts/molejo/clusteragent/v1alpha1/agent.proto).

## Boundaries

- Keep Kubernetes access bounded by the command contract and least-privilege RBAC.
- Never persist credentials, private keys, certificates, or kubeconfigs in logs or
  test snapshots.
- Read the [Cluster Agent guide](../../docs/en/platform/cluster-agent.md) and
  [threat model](../../docs/en/architecture/security-threat-model.md) before changing
  trust boundaries; use the [contribution guide](../../docs/en/CONTRIBUTING.md) for tests.
