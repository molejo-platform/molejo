# molejoctl development

`molejoctl` is the operator-facing CLI for Molejo lifecycle and explicit Day Zero
runbooks. It does not hide cluster creation, node administration, networking, or
cloud IAM.

## Explore

- Start at `main.go` and `cmd/` for the Cobra command tree and user-facing flags.
- Domain decisions live under `internal/foundation/`, `internal/platform/`, and
  `internal/capability/`.
- Keep planners and models pure. Place Kubernetes and Helm effects in the owning
  operator or client adapter, reusing `internal/kubecontext/` and
  `internal/helmclient/` for shared wiring.

## Boundaries

- Prefer a functional core for plans and validation, with external effects in the
  imperative command shell.
- Capability runbooks must expose inputs, plan, ownership, verification, and
  teardown implications.
- Read the [operational model](../../docs/en/architecture/operational-model.md),
  [platform lifecycle](../../docs/en/platform/lifecycle.md), and
  [capability guides](../../docs/en/capabilities/README.md) before changing commands.
- Follow the [contribution guide](../../docs/en/CONTRIBUTING.md) for tests and gates.
