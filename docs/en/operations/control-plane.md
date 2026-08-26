# Control Plane Operations

The control plane is pre-alpha. An Actor may select among its Workspace
memberships; `owner` can administer the hierarchy and `tester` is read-only.

## Local bootstrap

Start PostgreSQL with `just db-up`, then run `just db-migrate`. Generate Argon2id
hashes by piping a password to `go run ./services/control-plane-api/cmd/control-plane-api hash-password`.
Configure `FRUTO_OWNER_PASSWORD_HASH`, `FRUTO_TESTER_1_PASSWORD_HASH`, and
`FRUTO_TESTER_2_PASSWORD_HASH` outside Git, then run the `bootstrap` command.

The API uses `KUBECONFIG` for host execution and also requires
`FRUTO_EXPECTED_KUBE_CONTEXT`, `FRUTO_EXPECTED_KUBE_SERVER`, and
`FRUTO_EXPECTED_CLUSTER_UID`. It refuses to mutate the runtime when any identity
differs. In a cluster, set `FRUTO_IN_CLUSTER=true`, provide the expected cluster
UID, and provide the database Secret out of band.

Plain HTTP is accepted only by the explicit development profile on loopback or
`*.localhost`. For browser-trusted local TLS, run `just dev-tls-cert` once, then
run `just dev-api-tls` and `just dev-frontend-tls` in separate terminals. The
certificate and key remain under ignored `.local/certs`; `mkcert` installs its
local CA in the developer trust store. The HTTP equivalents are
`just dev-api-http` and `just dev-frontend-http`.

The Console administration page creates Workspaces, Projects, Environments, and
Apps exclusively through the authenticated REST API. AppDeployment creation is
scoped by the selected Workspace and requires an App and Environment from the
same Project. The API returns `404` for resources outside the Actor membership
boundary and `403` when a tester attempts a mutation.

Hierarchy migrations use versions 005 through 007: expand, deterministic
backfill, then contract constraints. Run `just hierarchy-backfill-expand` to
apply only version 005, followed by `just hierarchy-backfill-dry-run` to report
the exact links and compatibility resources to create. Use
`just hierarchy-backfill-apply` only after the approved export to apply the
remaining forward migrations and confirm that no nullable relationship remains.
The integrated migration Job invokes this same guarded apply flow: it reports
the pre-mutation plan before backfill and only applies the contract after the
post-backfill verification, followed by later migrations in version order. The
commands are idempotent and never print
connection credentials.

The public Console origin is `https://cloud.molejo.dev`. Repository access uses a
GitHub App, following an installation model instead of a classic OAuth App. Set
the exact setup URL to
`https://cloud.molejo.dev/api/v1/github/installations/callback` and the exact user
authorization callback to `https://cloud.molejo.dev/api/v1/github/callback`, with
wildcard matching disabled. Enable only read-only repository `Contents` access;
`Metadata` remains read-only by default. Do not enable webhooks, checks, write
permissions, Device Flow, or OAuth authorization during installation.

The API stores the Workspace installation and the immutable repository ID, but
does not persist GitHub user or installation tokens. The one-time user token is
revoked after ownership verification; installation tokens are minted on demand
and discarded after each request. Only an owner may connect,
disconnect, or change an App source; members can inspect the selected source.
One App has at most one repository, while multiple Apps may use the same
repository. Disconnect is rejected while any App still references the
installation.

Supply the App ID, Client ID, slug, client secret, and RSA private key through an
out-of-band `molejo-github-app` Secret. The deployment mounts the two credentials
as files and starts normally when this optional Secret does not exist; GitHub
endpoints then return `github_not_configured`. Use
`deploy/control-plane/github-app-secret.example.yaml` only as a shape reference
and never place real credentials in Git.

## Phase 6 local evidence

`just control-plane-e2e-kind` creates a disposable Kind cluster and proves both
topologies. The first runs the API binary with a temporary `KUBECONFIG` and Vite
on the host while PostgreSQL runs in Docker. The second runs the API, Console,
migration Job, Services, and HTTPRoutes in Kind.

Pure frontend decisions and HTTP clients run first as Node unit tests without
React or a DOM. React/jsdom integration tests then cover login, expired session,
form behavior, routing, and product states. The pinned Playwright suite is kept
to one strategic test: login and the complete create, observe, update, history,
and delete flow. The PostgreSQL-backed Go suite separately proves rejection of
expired and revoked sessions. The runner also
proves pending-operation recovery after an API restart, `Unknown` while the Kind
API server is paused, idempotency under repetition and concurrency, and the
control-plane ServiceAccount with `kubectl auth can-i`. Secrets are generated at
runtime and diagnostics are collected before temporary resources are removed.

## Recovery and boundaries

Migrations are forward-only, applied by pinned Goose, and guarded by a
PostgreSQL session advisory lock. SQLC-generated types stay inside the
PostgreSQL adapter. A failed migration Job can be rerun explicitly after
inspecting its logs. A missing or unavailable runtime is shown as `Unknown`; it is not
treated as a successful deployment. This installation does not claim HA or DR.

Before a k3s acceptance, `just ci` must pass from a clean checkout. Record the
source commit, `linux/amd64`, API, Console and Testkit digests in an external
release manifest; replace the cluster UID and trusted-proxy placeholders;
create database and bootstrap Secrets outside Git; and decide whether the
database is explicitly disposable. If it is not disposable, create and verify
an encrypted manual export before rollout. Rollback means reapplying compatible
image digests; a forward migration that is not understood by the previous
binary blocks rollback. Recovery is manual restore into a separate PostgreSQL
instance followed by `SchemaReady` and a functional check. This is not
production disaster recovery.

Run the read-only `just control-plane-preflight-k3s` with the approved context,
server, cluster UID and image digests. After it succeeds, use
`just control-plane-render-release` to render the digest-pinned Kustomize output
to an absolute path outside the checkout. Neither command applies resources;
cluster mutation remains a separately authorized manual step.

## Phase 7 k3s acceptance

Use a clean committed checkout and an external directory with mode `0700`. The
lab overlay pins PostgreSQL 17.6 to its `linux/amd64` manifest digest, schedules
it on `fruto-data-01`, requests a 2 GiB `local-path` volume, and marks both the
service and storage as disposable pre-alpha fixtures. It is not HA or a managed
database.

```bash
export FRUTO_RELEASE_DIR=/private/tmp/molejo-control-plane-release
export FRUTO_K3S_CONTEXT=fruto-lab
export FRUTO_EXPECTED_KUBE_SERVER='<approved-kube-api-url>'
export FRUTO_EXPECTED_CLUSTER_UID='<approved-kube-system-uid>'
export FRUTO_TRUSTED_PROXY_CIDR='<approved-pod-cidr>'
export FRUTO_TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit@sha256:1b5a36a776cc16dd3fa728c2269109ca45fca2a4af622b3a166e4e578b9cdb08

just ci
just control-plane-build-release
source "$FRUTO_RELEASE_DIR/images.env"
just control-plane-preflight-k3s
export FRUTO_RELEASE_OUTPUT="$FRUTO_RELEASE_DIR/control-plane.yaml"
just control-plane-render-release
just control-plane-prepare-k3s
just control-plane-apply-k3s
just control-plane-accept-k3s
```

The preparation step generates the owner password, its Argon2id hash, and the
PostgreSQL credentials under `$FRUTO_RELEASE_DIR/secrets` with mode `0600`; only
Secret references reach Pod specs. It also copies the existing registry pull
credential into `fruto-control-plane` without writing it to the repository or
terminal. Retrieve the owner password locally for the browser acceptance and
rotate it after the test. Do not paste passwords, hashes, database URLs, Secret
data, or kubeconfigs into issues, logs, commits, or chat.

After automated acceptance, use the Console to create the recorded Testkit
digest as `Public`, wait for `Ready`, update it, inspect history, restart
`deployment/control-plane-api`, reload the same browser session, and delete the
deployment. Record only public IDs, image digests, operation states, route
conditions, and resource counts. A second application of the same release
bundle is the first-release rollback proof; a different previous digest may be
reapplied only when its binary understands every applied forward migration.

Because this lab database is explicitly disposable, recovery means recreating
the fixture, rerunning migrations and bootstrap, and proving `SchemaReady` plus
the functional flow. If the installation is changed to non-disposable, stop and
prove an encrypted manual export and a restore into a separate PostgreSQL
instance before rollout. Neither path is production disaster recovery.

Replica, CPU, and memory maxima are product quotas enforced by the public API.
The CRD deliberately enforces only runtime validity and request/limit relations;
it does not duplicate those product quotas.

The repository's NetworkPolicy is an incomplete, non-installed example. Its
PostgreSQL egress is intentionally not destination-scoped because the portable
installation has no database destination contract yet. Do not install it as-is;
select the database destination and CNI first, then validate the resulting policy.
