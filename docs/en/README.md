# Molejo

[Project home](../../README.md) | [Português (Brasil)](../pt-BR/README.md) |
[Español (Argentina)](../es-AR/README.md)

> Experimental pre-alpha project. Molejo is not ready for production.

Molejo is a public and portable Kubernetes Application Platform. It aims
to let people create, publish, and operate applications without requiring them to
understand Kubernetes, `kubectl`, YAML, or the underlying infrastructure.

Kubernetes is the execution substrate, not the product API. Users declare product
intent through Molejo contracts, and trusted controllers reconcile that intent into
Kubernetes resources.

## Status

The repository is at its foundation stage. Work is intentionally incremental: the
smallest useful capability is implemented, observed in execution, corrected from
real evidence, and only then extended.

The repository now contains an executable vertical slice: a pre-alpha product
API and Console, an `AppDeployment` contract, a Kubernetes operator, a private
ClusterIP Service, optional publication through HTTPRoute and a shared HTTPS
Gateway, GitHub App repository sources, exact-commit Builds, immutable
digest-pinned Releases, and reproducible integration and end-to-end tests. The
first build contract accepts a root `Dockerfile` for `linux/amd64` through a
separate rootless BuildKit service. It does not yet provide external DNS
automation or production-ready workload and build isolation.

## Product Model

The canonical hierarchy is:

```text
Workspace → Project → App + Environment
```

`App` and `Environment` are siblings beneath the same Project. An `App` is the
logical application identity. An `AppEnvironment` owns the branch and runtime
configuration for one App in one Environment. A `Deployment` is an immutable
record of a Release and configuration revision applied to that target. The
revision is created only when runtime configuration changes and is selected
explicitly together with the Release after an impact preview. The Kubernetes
`AppDeployment` is an internal runtime projection.

The platform remains the source of truth for product identity, ownership, and
authorization. Kubernetes names, namespaces, labels, and annotations are runtime
projections and never grant product permissions.

## Monorepo

This repository is the public Molejo monorepo. It will contain the
versioned contracts and the components that implement the public product.

The structure will be introduced only when each component has a real consumer:

- `apps/` — applications used directly by people or software agents;
- `services/` — independently runnable server-side components;
- `packages/` — reusable libraries, Kubernetes contracts, and generated SDKs;
- `contracts/` — language-neutral canonical interface definitions;
- `deploy/` — generated and maintained Kubernetes installation artifacts;
- `test/e2e/` — tests that cross component boundaries;
- `docs/` — public architecture and project documentation.

The initial Go codebase will use one module at the repository root. Additional Go
modules and a `go.work` file will be introduced only when a component, such as a
public SDK, requires independent versioning and release compatibility.

## Technology Direction

- Go, Kubebuilder, and `controller-runtime` for Kubernetes controllers;
- Go, `net/http`, and Chi for HTTP APIs;
- TypeScript, React, Vite, Tailwind CSS, shadcn/ui, and Lineicons for the web UI;
- Buildx and BuildKit for container builds;
- a root `justfile` for local development commands.

These choices describe the initial direction. Components are added incrementally
and are not scaffolded before their phase begins.

## Development

The current checkout requires Go 1.26 or newer, Node.js 24.19.0 with Corepack,
Docker with Buildx, `kubectl`, and `just`. The local gate intentionally requires
the exact Node version pinned in `.node-version`. Kind does not need to be
installed globally; the end-to-end command runs its pinned version through Go.

```bash
just generate  # regenerate DeepCopy, CRD, RBAC, and Console API artifacts
just test      # run tests against a local envtest API server
just verify    # generate, check formatting, run go vet, and run tests
just e2e       # validate private and public routing in a disposable Kind cluster
just ci        # run the complete deterministic local gate
just e2e-public # separately verify outbound public HTTPS access
just frontend-check # type-check the React fixture and Console from the pnpm lockfile
just frontend-test # run Console tests and validate both frontend images in a restricted container
just audit-frontend-images # run the optional Docker Scout vulnerability check
```

`just ci` checks tracked generation, runs `just verify`, executes the
PostgreSQL-backed control-plane integration suite, validates both control-plane
topologies in a disposable Kind cluster, and then runs the platform Kind E2E.

The first run downloads pinned Go modules, envtest binaries, Kind, and container
images. `just e2e` uses a temporary kubeconfig and does not access the currently
selected Kubernetes context. It builds two local versions of the HTTP fixture,
addresses them by digest, and validates private HTTP plus HTTPS publication of
REST, GraphQL, SSE, and WebSocket through a local Gateway and trusted ephemeral
certificate. It also validates static hosting, SPA deep links, cache behavior,
probes, rollout, drift, self-healing, and garbage collection. `just e2e-public`
adds only a real outbound HTTPS request and remains
outside the deterministic `just ci` gate. Public DNS and a publicly trusted
certificate require separate acceptance in the foundation environment.

`just audit-frontend-images` is deliberately outside `just ci`. It requires
Docker Scout and uses its mutable vulnerability database to check critical and
high operating-system and npm findings in the discarded SPA builder and performs
a full critical/high scan of both runtime images.

The manual `just e2e-frontend-k3s` target is reserved for maintainers with access
to `fruto-lab`. It deploys private-registry images by digest and leaves
`static.molejo.dev` and `spa.molejo.dev` available for inspection.

## Operations

The [platform operator runbook](operations/platform-operator.md) documents its
state contract, diagnostic workflow, protected metrics, and optional tracing.
The [control plane runbook](operations/control-plane.md) documents local TLS and
the authorized Phase 7 and Phase 8 k3s release, build, and recovery workflows.

## Documentation

English is the canonical documentation language. Portuguese (`pt-BR`) and
Argentinian Spanish (`es-AR`) versions are maintained alongside it, and more
languages may be added later.

Architecture Decision Records live in [`adr`](adr/README.md). ADR files
use the same identifier, filename, English title, and English section headings in
every language; only the text beneath those headings is localized.

## Contributing

Contribution guidelines will be published in [CONTRIBUTING.md](CONTRIBUTING.md).

## Community

- [Code of Conduct](../../CODE_OF_CONDUCT.md)
- [Security Policy](../../SECURITY.md)
- [Support](../../SUPPORT.md)
- [Maintainers](../../MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
