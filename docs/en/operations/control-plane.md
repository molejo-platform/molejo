# Control Plane Operations

The control plane is pre-alpha and intended for one local beta Workspace.

## Local bootstrap

Start PostgreSQL with `just db-up`, then run `just db-migrate`. Generate Argon2id
hashes by piping a password to `go run ./services/control-plane-api/cmd/control-plane-api hash-password`.
Configure `FRUTO_OWNER_PASSWORD_HASH`, `FRUTO_TESTER_1_PASSWORD_HASH`, and
`FRUTO_TESTER_2_PASSWORD_HASH` outside Git, then run the `bootstrap` command.

The API uses `KUBECONFIG` for host execution. In a cluster, set
`FRUTO_IN_CLUSTER=true` and provide the database Secret out of band.

## Phase 6 local evidence

`just control-plane-e2e-kind` creates a disposable Kind cluster and proves both
topologies. The first runs the API binary with a temporary `KUBECONFIG` and Vite
on the host while PostgreSQL runs in Docker. The second runs the API, Console,
migration Job, Services, and HTTPRoutes in Kind.

The pinned Playwright suite validates login, the complete deployment flow,
history, deep links, missing session cookies, and invalid credentials. The
PostgreSQL-backed Go suite separately proves rejection of expired and revoked
sessions. The runner also
proves pending-operation recovery after an API restart, `Unknown` while the Kind
API server is paused, idempotency under repetition and concurrency, and the
control-plane ServiceAccount with `kubectl auth can-i`. Secrets are generated at
runtime and diagnostics are collected before temporary resources are removed.

## Recovery and boundaries

Migrations are forward-only and guarded by PostgreSQL advisory/transactional
serialization. A failed migration Job can be rerun explicitly after inspecting
its logs. A missing or unavailable runtime is shown as `Unknown`; it is not
treated as a successful deployment. This installation does not claim HA or DR.

Replica, CPU, and memory maxima are product quotas enforced by the public API.
The CRD deliberately enforces only runtime validity and request/limit relations;
it does not duplicate those product quotas.

The repository's NetworkPolicy is an incomplete, non-installed example. Its
PostgreSQL egress is intentionally not destination-scoped because the portable
installation has no database destination contract yet. Do not install it as-is;
select the database destination and CNI first, then validate the resulting policy.
