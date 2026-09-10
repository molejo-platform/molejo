# Molejo

[Project home](../../README.md) | [Português (Brasil)](../pt-BR/README.md) |
[Español (Argentina)](../es-AR/README.md)

> Experimental alpha project. Molejo is not ready for production.

Molejo is a public and portable Kubernetes Application Platform. It aims
to let people create, publish, and operate applications without requiring them to
understand Kubernetes, `kubectl`, YAML, or the underlying infrastructure.

Kubernetes is the execution substrate, not the product API. Users declare product
intent through Molejo contracts, and trusted controllers reconcile that intent into
Kubernetes resources.

## Status

The repository remains experimental and in alpha. Its current implementation
includes versioned Kubernetes contracts, the Platform Operator, a control-plane
backend, and the outbound Cluster Agent pairing flow. These components are not a
supported production platform. Alpha releases may change contracts without a
compatibility or migration commitment.

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

Current architecture and component guides:

- [Operational model](architecture/operational-model.md)
- [Foundation inspection](foundation/inspect.md)
- [Platform lifecycle](platform/lifecycle.md)
- [Platform Operator](platform/platform-operator.md)
- [Outbound Cluster Agent](platform/cluster-agent.md)
- [Cluster capabilities](capabilities/README.md)
- [Application loop](application-loop/README.md)
- [External CI releases](application-loop/external-ci.md)
- [Security threat model](architecture/security-threat-model.md)
- [Outbound Cluster Agent identity and pairing ADR](adr/0013-outbound-cluster-agent-identity-and-pairing.md)
- [External CI release and deployment boundary ADR](adr/0014-external-ci-release-and-deployment-boundary.md)
- [Capability ownership ADR](adr/0015-capability-ownership.md)
- [Alpha lifecycle policy ADR](adr/0016-alpha-lifecycle-policy.md)
- [Human identity boundary ADR](adr/0017-human-identity-boundary.md)
- [Capability observation and feature availability ADR](adr/0018-capability-observation-and-feature-availability.md)
- [Workspace provisioning and namespace boundary ADR](adr/0019-workspace-provisioning-and-namespace-boundary.md)
- [Secret custody and runtime delivery ADR](adr/0020-secret-custody-and-runtime-delivery.md)
- [Explicit operator-managed bindings ADR](adr/0021-explicit-operator-managed-bindings.md)
- [Provider-neutral metrics with a Prometheus-compatible query adapter ADR](adr/0022-provider-neutral-metrics-with-prometheus-query.md)
- [Console frontend stack boundary ADR](adr/0023-console-frontend-stack-boundary.md)

## Contributing

Contribution guidelines will be published in [CONTRIBUTING.md](CONTRIBUTING.md).

## Community

- [Code of Conduct](../../CODE_OF_CONDUCT.md)
- [Security Policy](../../SECURITY.md)
- [Support](../../SUPPORT.md)
- [Maintainers](../../MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
