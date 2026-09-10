# Control Plane development

The Control Plane owns product identity, authorization, desired state, provider
bindings, operations, and the public API. It does not access Kubernetes directly.

## Explore

- Start at `cmd/control-plane-api/` for process composition.
- Use `internal/api/` for HTTP transport, `internal/application/` for composition,
  `internal/domain/` for product rules, and `internal/store/` for PostgreSQL.
- Agent transport lives in `internal/clusteragent/`; background execution stays in
  the owning worker package.
- The API contract is [`../../contracts/openapi/control-plane-v1.yaml`](../../contracts/openapi/control-plane-v1.yaml).

## Boundaries

- Keep domain decisions independent from HTTP, PostgreSQL, Kubernetes, and providers.
- Re-authorize and admit every mutation; runtime observations never grant product
  permissions.
- Reach clusters only through the authenticated Cluster Agent contract.
- Read the [ADR index](../../docs/en/adr/README.md) before changing ownership or
  public contracts, and follow the [contribution guide](../../docs/en/CONTRIBUTING.md)
  for generation and tests.
