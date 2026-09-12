# Molejo development instructions

Molejo manages applications on Kubernetes; Kubernetes is its execution substrate,
not its product API. Use this file as an index and load deeper context only when
the task requires it.

## Read by task

- Setup, generation, tests, and Git conventions: [`docs/en/CONTRIBUTING.md`](docs/en/CONTRIBUTING.md)
- Product model and repository overview: [`docs/en/README.md`](docs/en/README.md)
- Component boundaries: [`docs/en/architecture/operational-model.md`](docs/en/architecture/operational-model.md)
- Architecture decisions: [`docs/en/adr/README.md`](docs/en/adr/README.md)
- Authentication, authorization, RBAC, or secrets: [`docs/en/architecture/security-threat-model.md`](docs/en/architecture/security-threat-model.md)
- Documentation translations: [`docs/TRANSLATION_GLOSSARY.md`](docs/TRANSLATION_GLOSSARY.md)
- Platform commands and alpha lifecycle: [`docs/en/platform/lifecycle.md`](docs/en/platform/lifecycle.md)
- Conformance runner: [`tools/cmd/molejo-conformance/README.md`](tools/cmd/molejo-conformance/README.md)
- Release work: [`tools/cmd/release/README.md`](tools/cmd/release/README.md)

Before editing a component, read its local instructions:

- Console: [`apps/console-web/AGENTS.md`](apps/console-web/AGENTS.md)
- CLI: [`apps/molejoctl/AGENTS.md`](apps/molejoctl/AGENTS.md)
- Control Plane: [`services/control-plane-api/AGENTS.md`](services/control-plane-api/AGENTS.md)
- Cluster Agent: [`services/cluster-agent/AGENTS.md`](services/cluster-agent/AGENTS.md)
- Platform Operator: [`services/platform-operator/AGENTS.md`](services/platform-operator/AGENTS.md)
- Tooling: [`tools/AGENTS.md`](tools/AGENTS.md)

## Code map

- `apps/` — Console and `molejoctl` entry points.
- `services/` — Control Plane, Cluster Agent, and Platform Operator.
- `contracts/` — OpenAPI and Protobuf sources plus generated clients.
- `packages/` — shared domain and Kubernetes API packages.
- `deploy/` — Helm and Kubernetes distribution artifacts.
- `tools/` and `test/` — generation, release, conformance, and cross-component tests.

## Working rules

- Inspect the worktree and preserve unrelated user changes.
- Follow the nearest `AGENTS.md`; local instructions extend this file.
- Change authoritative contract sources, never generated outputs, and regenerate
  through the documented workflow.
- Keep changes within the owning domain and validate the narrowest relevant scope
  before broader gates.
- Treat `docs/plans/` as local planning material, not repository authority.
- Verify mutable state such as branches, releases, images, and clusters live.
- Do not push, publish, release, deploy, or mutate an external cluster unless the
  user explicitly requests it.
