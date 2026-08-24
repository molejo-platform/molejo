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
history, deep links, expired sessions, and invalid credentials. The runner also
proves pending-operation recovery after an API restart, `Unknown` while the Kind
API server is paused, idempotency under repetition and concurrency, and the
control-plane ServiceAccount with `kubectl auth can-i`. Secrets are generated at
runtime and diagnostics are collected before temporary resources are removed.

## Recovery and boundaries

Migrations are forward-only and guarded by PostgreSQL advisory/transactional
serialization. A failed migration Job can be rerun explicitly after inspecting
its logs. A missing or unavailable runtime is shown as `Unknown`; it is not
treated as a successful deployment. This installation does not claim HA or DR.

NetworkPolicy is intentionally outside the primary local gate until a compatible
CNI is explicitly selected and tested.
