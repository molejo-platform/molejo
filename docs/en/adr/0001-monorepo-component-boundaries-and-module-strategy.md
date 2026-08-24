# ADR-0001: Monorepo Component Boundaries and Module Strategy

## Status

Draft

## Context

Molejo will contain user interfaces, command-line tools, Kubernetes
controllers, HTTP APIs, workers, shared contracts, and generated SDKs in multiple
languages. Placing the first Go code directly in repository-wide `api/`, `cmd/`,
and `internal/` directories would make the initial scaffold simple, but would not
make component ownership, runtime boundaries, or intended consumers explicit as
the platform grows.

Directory boundaries, Go module boundaries, and build orchestration solve
different problems. A deployable component does not require its own Go module,
and a root `go.mod` does not imply that the repository contains a single root
application. Creating a module per component before independent versioning is
needed would add dependency synchronization, testing, and release overhead.

Molejo also treats an actor as either a person or a software agent. Actor-facing
entry points may therefore include web, desktop, TUI, and CLI applications, as
well as future protocol adapters used directly by agents. The placement of a
future MCP server remains uncertain because it may behave either as a thin
actor-facing adapter over existing APIs or as a server-side platform capability.

## Decision

The repository will organize source code by component responsibility:

- `apps/` contains entry points used directly by actors. Actors may be people or
  software agents. Examples include web, desktop, TUI, and CLI applications.
- `services/` contains independently runnable server-side platform components,
  such as HTTP APIs, Kubernetes operators, controllers, and workers.
- `packages/` contains reusable, non-deployable libraries and generated SDKs for
  internal or external consumers.
- `contracts/` contains language-neutral canonical interface definitions, such
  as OpenAPI documents, when those contracts exist.
- `deploy/` contains integrated installation artifacts when multiple components
  need to be installed together.
- `test/e2e/` contains tests that cross component boundaries.

Directories will be introduced only when they have a real component or consumer;
the complete future tree will not be scaffolded in advance.

Dependencies must point toward stable boundaries:

- applications and services may depend on packages and contracts;
- packages and contracts must not depend on applications or services;
- one service must not import another service's implementation;
- component-private Go code belongs in that component's `internal/` directory;
- cross-service interaction occurs through an explicit protocol or contract;
- shared packages are extracted only after a second real consumer or an external
  distribution requirement exists.

The repository will initially use one root Go module:

```text
module github.com/fruto-platform/fruto
```

Go source may live under `apps/`, `services/`, and `packages/` while remaining in
that module. Components may be built, tested, and containerized independently
without becoming independently versioned modules.

A new `go.mod` and a versioned root `go.work` will be introduced only when a Go
component, such as a public SDK, requires its own version, release compatibility,
or external distribution lifecycle. Independently released modules must also be
tested with workspace resolution disabled so that `go.work` cannot hide an
unpublished dependency.

The root `justfile` is the initial human-facing task runner across languages. A
pnpm workspace will be introduced with the first JavaScript or TypeScript
component. Turborepo may be added when multiple JavaScript or TypeScript packages
produce a real task graph or measurable caching need. Bazel or another polyglot
build system requires a separately demonstrated scaling problem.

The location of a future MCP server is deliberately not fixed by this decision.
A thin MCP adapter used directly by agents and delegating to existing platform
APIs may belong in `apps/`. An MCP component that owns a server-side platform
capability or lifecycle belongs in `services/`. In either case, it must not
duplicate domain authorization or business rules owned by the control plane.

## Consequences

Component purpose, ownership, deployability, and dependency direction become
visible from the repository structure. The first Kubernetes contract can live in
`packages/kubernetes-api`, the operator can later live in
`services/platform-operator`, and an end-user CLI can later live in `apps/cli`
without placing application code directly at the repository root.

All initial Go packages share one dependency and release boundary. This keeps
cross-component development and `go test ./...` simple, but a dependency upgrade
affects the shared module and Go packages cannot be versioned independently until
they are extracted into another module.

The structure depends on review discipline: generic shared packages, direct
service implementation imports, and premature empty directories would weaken the
boundaries. Build graph tooling will remain intentionally limited until the
repository contains enough components to justify it.

MCP placement remains a future decision based on the first concrete MCP use case.
This avoids treating a protocol name as an architectural layer before its runtime
and ownership responsibilities are known.

## Alternatives Considered

Keep global `api/`, `cmd/`, and `internal/` directories. This follows a common Go
repository layout but was not selected because it makes heterogeneous component
boundaries and actor-facing applications less explicit in this monorepo.

Create one Go module for every application, service, and package immediately.
This was not selected because the components initially share the `v0.0.1` release
lifecycle, and multiple modules would add version synchronization and standalone
module testing before independent releases exist.

Restrict `apps/` to human graphical interfaces. This was not selected because
CLI, TUI, and agent-facing entry points are also applications used directly by
platform actors.

Classify every MCP server as either an application or a service now. This was not
selected because MCP describes a protocol surface, not enough information to
determine runtime ownership or lifecycle.

Adopt Turborepo, Bazel, or another build graph tool from the beginning. This was
not selected because the initial repository has no task graph or build scale that
justifies the additional configuration and maintenance.

## References

- [Go multi-module workspaces](https://go.dev/doc/tutorial/workspaces)
- [Go module repository organization](https://go.dev/doc/modules/managing-source)
- [Turborepo package types](https://turborepo.dev/docs/core-concepts/package-types)
- [Turborepo package and task graphs](https://turborepo.dev/docs/core-concepts/package-and-task-graph)
