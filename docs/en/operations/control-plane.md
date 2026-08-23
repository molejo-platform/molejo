# Control Plane Operations

The control plane is pre-alpha and intended for one local beta Workspace.

## Local bootstrap

Start PostgreSQL with `just db-up`, then run `just db-migrate`. Generate Argon2id
hashes by piping a password to `go run ./services/control-plane-api/cmd/control-plane-api hash-password`.
Configure `FRUTO_OWNER_PASSWORD_HASH`, `FRUTO_TESTER_1_PASSWORD_HASH`, and
`FRUTO_TESTER_2_PASSWORD_HASH` outside Git, then run the `bootstrap` command.

The API uses `KUBECONFIG` for host execution. In a cluster, set
`FRUTO_IN_CLUSTER=true` and provide the database Secret out of band.

## Recovery and boundaries

Migrations are forward-only and guarded by PostgreSQL advisory/transactional
serialization. A failed migration Job can be rerun explicitly after inspecting
its logs. A missing or unavailable runtime is shown as `Unknown`; it is not
treated as a successful deployment. This installation does not claim HA or DR.
