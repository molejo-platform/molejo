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

It creates and deletes an isolated Kind cluster and registry, installs the same
charts used by `molejoctl`, and runs the compiled `alpha-core/v1` and
`http-publication/v1` profiles. The latter deploys an exact hostname, adds a
subdomain-pool hostname on the same application port, removes the exact address
while preserving the pool, and then withdraws the application. It uses a real
Gateway and locally trusted TLS. Profile and aggregate harness JSON/JUnit
evidence are retained in the printed private directory; build archives, binaries,
credentials, keys, certificates, and kubeconfig remain in deleted scratch space. The harness
requires Docker, Helm, OpenSSL, jq, and kubectl; it never uses the current
cluster context.

List or inspect the compiled contract without making changes:

```bash
go -C tools run ./cmd/molejo-conformance profile list
go -C tools run ./cmd/molejo-conformance plan --profile alpha-core \
  --cluster-id <cluster-id> --workspace-id <test-workspace-id>
```

Persistent targets must use a pre-existing test Workspace. Binding management
is accepted only for disposable targets. `run` records target identity before
effects and `cleanup --run-dir <directory>` can resume cleanup from the private
resource ledger. A persistent wildcard listener can be selected independently
from its ephemeral Exact domain with `--publication-exact-listener-hostname`.
The same compiled profile and fingerprint identify a Kind or K3s verdict;
installation, storage, Gateway, Registry, and metrics checks remain separate
operational prerequisites.

The dedicated workflow runs once per push, manual dispatch, and UTC day. Keep it
non-blocking while the alpha signal is calibrated; require it for changes to the
covered paths only after ten successful qualified runs on distinct days.

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

Public edge acceptance is also read-only and remains separate from the compiled
application profiles:

```bash
tools/testing/public-edge-acceptance.sh --output ./public-edge-evidence
```

It expects `molejo.dev` and `cloud.molejo.dev` to return HTTP 200,
`registry.molejo.dev/v2/` to return HTTP 401, and every served certificate to
remain valid for at least 14 days. Use `just molejo-conformance
registry-private ...` with the Registry setup file for the independent private
pull smoke. Optional `--apex-body-marker` and `--console-body-marker` arguments
verify endpoint identity without retaining response content; the report stores
only its SHA-256. The Registry check also requires the Bearer authentication
challenge. Neither command edits DNS, Gateway, routes, or certificates.

## Commits

- Use English Conventional Commit messages.
- Add a commit body when changing more than three files.
- Stage only files that belong to the change.
