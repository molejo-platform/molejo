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

The maintained end-to-end proof runs through the versioned conformance runner:

```bash
just molejo-conformance kind
```

It creates and deletes an isolated Kind cluster and Registry, installs the same
charts used by `molejoctl`, and runs the compiled `alpha-core/v1` and
`http-publication/v1` profiles with real Gateway and TLS behavior. Read the
[runner guide](../../tools/cmd/molejo-conformance/README.md) for profiles, target
modes, direct commands, evidence, exit codes, and cleanup recovery. Read the
[development guide](../../tools/cmd/molejo-conformance/DEVELOPMENT.md) before
changing profile semantics, reports, ownership, or harness boundaries.

The dedicated workflow runs once per push, manual dispatch, and UTC day. Keep it
non-blocking while the alpha signal is calibrated; require it for changes to the
covered paths only after ten successful qualified runs on distinct days.

## K3s acceptance checks

Maintainer checks against existing clusters are not part of `just verify`:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --file ./tls-setup.yaml
```

The control-plane script's `teardown` and `cycle` modes change cluster state and
require an explicit `--confirm <context>` argument. TLS verification is read-only.
Run the scripts without arguments to see their complete usage.

Public edge acceptance is also read-only and remains separate from the compiled
application profiles:

```bash
tools/testing/public-edge-acceptance.sh --output ./public-edge-evidence
```

The [test and acceptance guide](../../tools/testing/README.md) defines the
responsibility, prerequisites, mutation boundary, and retained evidence for each
Kind, K3s, Registry, Gateway, TLS, storage, metrics, and public-edge harness.

## Commits

- Use English Conventional Commit messages.
- Add a commit body when changing more than three files.
- Stage only files that belong to the change.
