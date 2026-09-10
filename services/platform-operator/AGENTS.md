# Platform Operator development

The Platform Operator reconciles closed Molejo Kubernetes APIs into owned runtime
resources. It does not own product identity, authorization, provider automation,
or the public API.

## Explore

- Start at `cmd/manager/` for process composition.
- Controllers and deterministic projections live in `internal/controller/`;
  namespace and fixed RBAC reconciliation lives in `internal/workspaceboundary/`.
- Kubernetes API sources live in `../../packages/kubernetes-api/apis/`; generated
  CRDs live in `../../deploy/crds/`.

## Boundaries

- Keep rendering and status decisions deterministic and separate from controller
  side effects.
- Reconcile only resources covered by Molejo contracts and explicit ownership.
- Read the [Platform Operator guide](../../docs/en/platform/platform-operator.md)
  before changing projections, and follow the
  [contribution guide](../../docs/en/CONTRIBUTING.md) for generation and tests.
