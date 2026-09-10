# Documentation translation glossary

English is the canonical documentation language. This glossary keeps product
concepts and operational contracts consistent in Brazilian Portuguese and
Argentinian Spanish; it does not replace semantic review.

## Editorial rules

- Do not translate commands, flags, API paths, field names, type names, resource
  names, protocol values, or literal status and reason values.
- Preserve the official capitalization of Molejo components and product types.
- Translate explanatory prose without weakening requirements, invariants,
  security controls, or failure behavior.
- Use the preferred natural-language form below. Keep the canonical English form
  when the term identifies a product type or contract.

## Preferred terms

| Canonical English | Portuguese (Brazil) | Spanish (Argentina) | Usage |
| --- | --- | --- | --- |
| Workspace | Workspace | Workspace | Product type; do not translate. |
| Project | Project | Project | Product type; do not translate. |
| App | App | App | Product type; do not translate. |
| Environment | Environment | Environment | Product type; do not translate. |
| AppEnvironment | AppEnvironment | AppEnvironment | Product type; do not translate. |
| Release | Release | Release | Product type; preserve capitalization when referring to the type. |
| Deployment | Deployment | Deployment | Distinguish the Molejo type from a Kubernetes `Deployment` by context. |
| Capability | Capability | Capability | Product contract; plural forms may follow the surrounding language. |
| Feature Availability | Feature Availability | Feature Availability | Read-only product projection; never implies actor permission. |
| Binding | Binding | Binding | Explicit product contract; do not translate. |
| Provider | Provider | Provider | Typed integration contract; do not translate. |
| Foundation | Foundation | Foundation | Day-zero substrate category; do not translate. |
| Application Loop | ciclo da aplicação | ciclo de la aplicación | Use English capitalization only when naming the category. |
| Platform Lifecycle | ciclo de vida da plataforma | ciclo de vida de la plataforma | Use English capitalization only when naming the category. |
| Control Plane | Control Plane | Control Plane | Official component name; lower case is allowed only for a generic or provider control plane. |
| Cluster Agent | Cluster Agent | Cluster Agent | Official component name. |
| Platform Operator | Platform Operator | Platform Operator | Official component name. |
| cluster | cluster | clúster | Natural-language form; preserve `Cluster` for the product type. |
| workload | workload | workload | Kubernetes runtime concept; keep in English. |
| reconciliation | reconciliação | reconciliación | Process; preserve literal API or metric names. |
| enrollment | enrollment | enrollment | Agent protocol operation; keep in English. |
| placement | placement | placement | Workspace-to-cluster contract; keep in English. |
| readiness | prontidão | estado de preparación | Natural-language explanation; preserve literal readiness states. |
| current metrics | métricas atuais | métricas actuales | Bounded live telemetry, distinct from historical telemetry. |
| historical metrics | métricas históricas | métricas históricas | Provider-backed telemetry; never describe current metrics as its fallback. |
| secret | secret | secret | Product value; use Kubernetes `Secret` only for the resource type. |
| trust bundle | bundle de confiança | bundle de confianza | Protocol material; preserve `trustBundleId`. |
| ownership | ownership | ownership | Resource-control invariant; keep in English. |

When a new recurring domain term appears, add it here before introducing
different translations across documents.
