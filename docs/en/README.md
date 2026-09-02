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

The repository remains experimental and pre-alpha. Its current implementation
includes versioned Kubernetes contracts, the Platform Operator, a control-plane
backend, and the outbound Cluster Agent pairing flow. These components are not a
supported production platform or evidence of a supported public release.

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

This repository is the public Molejo monorepo. Its current structure is:

- `contracts/` — language-neutral and generated versioned contracts;
- `packages/` — shared libraries with concrete consumers;
- `services/` — the Platform Operator, control plane, and Cluster Agent;
- `deploy/` — generated and maintained Kubernetes installation artifacts;
- `docs/` — public architecture and operations documentation.

The Go codebase uses one module at the repository root. Additional Go
modules and a `go.work` file will be introduced only when a component, such as a
public SDK, requires independent versioning and release compatibility.

## Technology Direction

- Go, Kubebuilder, and `controller-runtime` for Kubernetes controllers;
- Go, `net/http`, and Chi for HTTP APIs;
- Protocol Buffers and gRPC for the authenticated Cluster Agent channel;
- Buildx and BuildKit for container builds;
- a root `justfile` for local development commands.

These choices describe the initial direction. Components are added incrementally
and are not scaffolded before their phase begins.

## Documentation

English is the canonical documentation language. Portuguese (`pt-BR`) and
Argentinian Spanish (`es-AR`) versions are maintained alongside it, and more
languages may be added later.

Current component guides:

- [Platform Operator](operations/platform-operator.md)
- [Outbound Cluster Agent](operations/cluster-agent.md)
- [Cluster TLS](operations/tls.md)
- [K3s day-zero setup](operations/cluster-setup.md)
- [Application registry access](operations/registry-access.md)
- [Outbound Cluster Agent identity and pairing ADR](adr/0013-outbound-cluster-agent-identity-and-pairing.md)

## Contributing

Contribution guidelines will be published in [CONTRIBUTING.md](CONTRIBUTING.md).

## Community

- [Code of Conduct](../../CODE_OF_CONDUCT.md)
- [Security Policy](../../SECURITY.md)
- [Support](../../SUPPORT.md)
- [Maintainers](../../MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
