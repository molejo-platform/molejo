# Contributing

## Prerequisites

- Go 1.26.6, the version used by CI.
- Node.js 24 or newer with Corepack enabled.
- `just` 1.57 or newer.
- Docker for PostgreSQL integration tests.
- `kubectl` only for acceptance checks against a real cluster.

The first test run downloads Go modules and the Kubernetes `envtest` binaries.
Install the Console dependencies with `corepack pnpm install --frozen-lockfile`.
Use `just --list` to discover the maintained commands.

## Generated code

Edit authoritative sources and run `just generate`; do not edit generated files
directly:

- `contracts/molejo/clusteragent/v1alpha1/agent.proto` generates the Agent Go
  and gRPC contract.
- `contracts/openapi/control-plane-v1.yaml` generates the Control Plane Go
  server types and Console TypeScript types.
- `packages/kubernetes-api/apis/` generates deep-copy code and `deploy/crds/`.
- Control Plane migrations and queries generate
  `services/control-plane-api/internal/store/sqlc/`.

Always inspect the generated diff before committing it.

## Tests

Run the fast test suite without external services:

```bash
just test
```

PostgreSQL integration tests require a running Docker daemon. Testcontainers
starts PostgreSQL 17.6 on a random host port and removes it after each package
suite:

```bash
just integration-test
```

To use an existing PostgreSQL instance instead of Docker, provide its URL:

```bash
MOLEJO_TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' just integration-test
```

`just verify` runs both suites and all repository quality gates.
`just ci` additionally checks that generation and formatting leave the
worktree unchanged.

Use the narrowest relevant command while iterating:

```bash
just operator-test
just cluster-agent-test
just control-plane-test
just contract-test
just distribution-test
```

The Console can also be checked independently:

```bash
just frontend-check
just frontend-test
just frontend-build
```

## Testing strategy

- Platform Operator: pure rendering tests, then `envtest`, with Kind only for
  behavior that requires a real cluster.
- Cluster Agent: table-driven decisions, real TLS with in-memory gRPC, and fake
  Kubernetes clients only for exact persistence effects.
- Control Plane: pure domain tests and controlled service integrations; use real
  PostgreSQL for transactions, constraints, migrations, concurrency, and
  idempotency.
- Console: static checks plus Vitest and Testing Library integration tests; use
  Playwright only when the browser or complete frontend/backend wiring is the
  behavior under test.

Prefer observable outcomes and eventual assertions over implementation details
and fixed sleeps. Add broad end-to-end coverage only for behavior that cannot be
proved at a lower layer. Never include credentials, private keys, certificates,
or kubeconfigs in snapshots.

## K3s acceptance checks

These maintainer checks are not part of `just verify` because they require an
existing cluster:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --file ./tls-setup.yaml
```

The control-plane script's `teardown` and `cycle` modes change cluster state and
require an explicit `--confirm <context>` argument. TLS verification is read-only.
Run the scripts without arguments to see their complete usage.

## Commits

- Use English Conventional Commit messages.
- Add a commit body when changing more than three files.
- Stage only files that belong to the change.
