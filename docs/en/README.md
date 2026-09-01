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

The current scope is limited to engineering conventions, architecture decisions,
and the first versioned contracts. It does not yet provide a functional platform,
public API, controller, or web interface.

## Product Model

The canonical hierarchy is:

```text
Workspace → Project → Environment → App
```

An `App` is the logical application identity. An `AppDeployment` represents the
deployment of a specific release of an App into an Environment.

The platform remains the source of truth for product identity, ownership, and
authorization. Kubernetes names, namespaces, labels, and annotations are runtime
projections and never grant product permissions.

## Monorepo

This repository is the public Molejo monorepo. It will contain the
versioned contracts and the components that implement the public product.

The structure will be introduced only when each component has a real consumer:

- `api/` — Kubernetes API types and versioned contracts;
- `cmd/` — Go entry points for controllers, APIs, and other binaries;
- `internal/` — private shared Go implementation;
- `web/` — the product web interface;
- `config/` — generated and maintained Kubernetes installation artifacts;
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

## Documentation

English is the canonical documentation language. Portuguese (`pt-BR`) and
Argentinian Spanish (`es-AR`) versions are maintained alongside it, and more
languages may be added later.

Architecture Decision Records live in [`adr`](adr/README.md). ADR files
use the same identifier, filename, and English section headings in every language;
only their content is localized.

## Contributing

Contribution guidelines will be published in [CONTRIBUTING.md](CONTRIBUTING.md).

## Community

- [Code of Conduct](../../CODE_OF_CONDUCT.md)
- [Security Policy](../../SECURITY.md)
- [Support](../../SUPPORT.md)
- [Maintainers](../../MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
