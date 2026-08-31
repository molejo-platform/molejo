# Molejo

> Experimental pre-alpha project. Molejo is not ready for production.

Molejo is a public and portable Kubernetes Application Platform. It aims
to let people create, publish, and operate applications without requiring them to
understand Kubernetes, `kubectl`, YAML, or the underlying infrastructure.

This repository is the public monorepo for the product. Kubernetes is its execution
substrate, while Molejo contracts and APIs represent the product intent exposed to
users.

## What you can reproduce today

The current vertical slice implements this reconciliation loop:

```text
AppDeployment v1alpha1 -> platform-operator -> Deployment + ClusterIP Service
                                             -> optional HTTPRoute/TCPRoute -> shared Gateway
                                             -> status
```

The operator creates and maintains one Kubernetes Deployment and one private
ClusterIP Service for each `AppDeployment`. Workloads may declare up to eight
named TCP ports and publish at most one HTTP and one experimental TCP endpoint;
private workloads keep only their internal Service. The operator projects immutable images, resources, probes,
and a restricted container runtime, reports rollout and publication state through
Conditions, and exposes protected metrics plus optional OpenTelemetry tracing.
The repository also proves static HTML and Vite/React SPA image contracts that
reuse this API without a frontend-specific CRD field. It includes unit tests,
Kubernetes API integration tests, container checks, and a disposable Kind
end-to-end environment.

The pre-alpha control plane also models Workspace/Project/App/Environment,
durable AppEnvironments, immutable Deployment history, GitHub App repository
sources, exact-commit Builds, bounded logs, and digest-pinned Releases. An owner
can build the AppEnvironment branch from a repository root `Dockerfile` for
`linux/amd64` through a separate rootless BuildKit service and deploy a promoted
Release to that configured target. The deterministic gate validates the
contracts and failure behavior; it does not claim that the external builder,
registry, DNS, or k3s deployment is live.

The local environment validates Gateway routing and TLS with an ephemeral trusted
certificate. Public DNS and a publicly trusted certificate remain an external
foundation acceptance step and are not claimed by the repository gate.

## Prerequisites

Install the following tools before cloning the repository:

- Git.
- Go 1.26.x. The container build is pinned to Go 1.26.7.
- Node.js 24.19.0 with Corepack. The local gate intentionally requires the exact
  version pinned in `.node-version`; frontend builds use pnpm 11.23.0.
- [just](https://github.com/casey/just), used as the repository task runner.
- Docker Engine or Docker Desktop with Buildx enabled.
- `kubectl` compatible with Kubernetes 1.36.
- Bash, `curl`, OpenSSL, `grep`, `sed`, `shasum`, `tar`, and common POSIX
  command-line tools.

Kind, `controller-gen`, and the envtest binaries do not require global
installation. Their versions are resolved by the repository on first use.
The first execution requires network access to download Go modules, test binaries,
and container images.

Docker Scout is optional and required only by `just audit-frontend-images`. The
vulnerability audit is deliberately separate from the deterministic local gate.

Confirm the main prerequisites:

```bash
go version
just --version
docker info
docker buildx version
kubectl version --client
node --version
corepack pnpm --version
```

## Verify the current development checkout

The implementation described by this README is developed on the
`release/v0.0.1` branch. From that checkout, run:

```bash
git switch release/v0.0.1
just verify
```

Before the first public release, this onboarding must be revalidated from a clean
clone of the exact release commit.

`just verify` performs the fast local validation cycle:

1. Regenerates DeepCopy code, the CRD, and RBAC.
2. Checks Go formatting.
3. Runs `go vet`.
4. Runs unit and envtest integration tests against Kubernetes 1.36.2.
5. Type-checks the React fixture and validates both frontend images under a
   non-root, read-only container runtime.

The command should finish without changing generated files. If it does change
them, inspect and version the generated output together with the source change.

## Reproduce the Kubernetes flow

Run the complete disposable environment with:

```bash
just e2e
```

The E2E script:

1. Creates a temporary Kubernetes 1.36.1 cluster with Kind 0.32.0.
2. Builds the operator, backend fixtures, static HTML, and two Vite/React SPA
   releases with Buildx and addresses workload images by digest.
3. Installs a local Gateway API and Traefik fixture with an ephemeral wildcard
   certificate trusted by the test client.
4. Checks private Service mapping, HTTP behavior, probes, workload security, and
   controlled egress.
5. Validates public REST, GraphQL, streaming SSE, persistent WebSocket, route
   removal, health, readiness, authenticated metrics, reconciliation, rollout,
   scaling, drift correction, child recreation, status transitions, deletion,
   and garbage collection.
6. Validates static private-to-public hosting, SPA deep links, cache policy,
   missing-asset `404`, and an in-place SPA release rollout.
7. Removes the cluster and temporary kubeconfig on success or failure.

The script uses an isolated kubeconfig and does not change your current Kubernetes
context. When a test fails, it prints Pods, resources, Events, and operator logs
before cleanup.

## Run the complete local gate

Before sharing a change, run:

```bash
just ci
```

This is the canonical local CI entry point. It verifies every tracked generated
artifact, runs `just verify`, exercises the PostgreSQL-backed control-plane
integration suite, validates both control-plane topologies in a disposable Kind
cluster, and finally runs the platform Kind E2E. There is currently no remote CI
pipeline or release publication attached to this command.

The deterministic gate uses an in-cluster controlled upstream. To separately
prove outbound access to a real public HTTPS endpoint, run when external
networking is available:

```bash
just e2e-public
```

The default target is `https://api.github.com/zen`; override it with
`E2E_PUBLIC_EGRESS_URL`. This checks workload egress only; it does not validate
inbound public DNS or a production certificate. The external check is
intentionally outside `just ci`.

To run the mutable vulnerability-database check separately:

```bash
just audit-frontend-images
```

This requires Docker Scout. It checks critical and high operating-system and npm
packages in the discarded SPA builder and performs a full critical/high scan of
both runtime images. It is intentionally outside `just ci` because vulnerability
databases change independently of the repository.

Maintainers with access to the personal cluster can run
`just e2e-frontend-k3s`. This separate manual target publishes amd64 fixtures to
the private registry, deploys them by digest through the explicit `fruto-lab`
context, and leaves `static.molejo.dev` and `spa.molejo.dev`
available for browser inspection. Its detailed prerequisites and persistent side
effects are documented in the operator runbook.

## Common commands

| Command | Purpose |
| --- | --- |
| `just fmt` | Format Go source files. |
| `just generate` | Regenerate Kubernetes API, CRD, RBAC, and Console API artifacts. |
| `just test` | Run unit and envtest integration tests. |
| `just frontend-check` | Install from the lockfile and type-check the React fixture. |
| `just frontend-test` | Validate both frontend images in a restricted Docker runtime. |
| `just audit-frontend-images` | Run the optional Docker Scout vulnerability check. |
| `just verify` | Run generation, formatting checks, vet, and tests. |
| `just e2e` | Exercise the complete flow in a disposable Kind cluster. |
| `just e2e-public` | Add a non-deterministic outbound public HTTPS check to the E2E flow. |
| `just e2e-frontend-k3s` | Manually publish and validate frontend fixtures on `fruto-lab`. |
| `just control-plane-build-release` | Publish the API, Console, platform operator, and Cluster Agent as `linux/amd64` digests. |
| `just control-plane-preflight-k3s` | Validate the approved k3s target without mutations. |
| `just control-plane-render-release` | Render the external digest-pinned CRD, operator, and control-plane bundle. |
| `just control-plane-prepare-k3s` | Generate external lab credentials and Kubernetes Secrets. |
| `just control-plane-apply-k3s` | Apply migrations, bootstrap, workloads, and routes in order. |
| `just control-plane-accept-k3s` | Check release digests, routes, RBAC, TLS, and redirect. |
| `just openbao-prepare-k3s` | Apply the lab OpenBao configuration and prepare versioned control-plane references. |
| `just observability-prepare-k3s` | Prepare immutable observability credential versions outside Git. |
| `just observability-render-release` | Render the commit-identified observability bundle. |
| `just observability-apply-k3s` | Apply the observability release without imperative restarts. |
| `just observability-accept-k3s` | Verify collector health and the OTLP-to-ClickHouse write/read path. |
| `just configuration-gc-k3s` | Explicitly collect unreferenced platform configuration versions after acceptance. |
| `just builds-build-release` | Publish the Phase 8 build worker as a `linux/amd64` digest. |
| `just builds-render-release` | Render the external digest-pinned build-plane bundle. |
| `just builds-prepare-k3s` | Prepare BuildKit mTLS and external build credentials on `fruto-lab`. |
| `just builds-apply-k3s` | Apply the separately authorized build plane on `fruto-lab`. |
| `just ci` | Run the complete local acceptance gate. |

## Repository layout

```text
deploy/                         Kubernetes manifests and examples
apps/                           User-facing Console entry points
contracts/                      Language-neutral public API contracts
packages/kubernetes-api/       Versioned Kubernetes API contracts
services/control-plane-api/     Product API, operation executor, and build worker
services/platform-operator/    AppDeployment controller and manager
test/                           Backend/frontend fixtures, generated checks, and end-to-end tests
docs/                           Architecture, operations, and localized documentation
```

The monorepo will add `apps/`, `services/`, and `packages/` components as their
independent boundaries become necessary. A `go.work` file will be introduced only
when the repository contains more than one Go module.

## Troubleshooting

- If envtest cannot open a local port, run the tests outside a restricted sandbox
  and confirm that local loopback connections are allowed.
- If BuildKit reports `no space left on device`, inspect Docker storage with
  `docker system df` before deciding which unused cache or images can be removed.
- If the first run appears slow, check downloads before treating it as a test
  failure; Go modules, envtest, Kind, and container images are cached afterward.

## Documentation

Choose a language:

- [English](docs/en/README.md)
- [Português (Brasil)](docs/pt-BR/README.md)
- [Español (Argentina)](docs/es-AR/README.md)

English is the canonical source. Localized documentation mirrors the same relative
structure whenever an equivalent page is available.

## Contributing

Contribution guidelines will be added soon. See [CONTRIBUTING.md](CONTRIBUTING.md)
for the language-specific entry points.

## Community

- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security Policy](SECURITY.md)
- [Support](SUPPORT.md)
- [Maintainers](MAINTAINERS.md)
- [Apache License 2.0](LICENSE)
