# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Context

Aplicar intención de producto en Kubernetes cruza dos sistemas sin una
transacción distribuida. Retries, requests HTTP duplicadas y reinicios de la API
deben representarse explícitamente.

## Decision

PostgreSQL es autoritativo para identidades Actor, memberships de Workspace, la
jerarquía Workspace/Project/App/Environment, configuración de AppEnvironment,
historial inmutable de Deployment, IDs públicos, idempotencia, sesiones e
historial de operaciones. Constraints compuestas garantizan que App y Environment vinculados por un AppEnvironment
pertenezcan al mismo Project y Workspace. El CRUD relacional de la jerarquía es
síncrono y transaccional; las mutaciones con efecto en runtime persisten intención
y operación en una misma transacción antes de ese efecto.

Las operaciones usan lease, worker ID, fencing token, versión deseada y backoff
limitado. Solo `EnsureWorkspace`, `ApplyDeployment` y `DeleteAppEnvironment`
cruzan el límite de runtime. Delete actúa como barrera contra updates
posteriores. Kubernetes solo es autoritativo para el estado observado; runtime
indisponible o antiguo se informa como `Unknown` o `Progressing`, nunca como
`Ready` actual.

## Consequences

El executor puede retomar después de un crash y requests duplicadas son seguras.
El schema es forward-only. Como este pre-alpha no tiene workloads críticos, la
migration 011 descarta intencionalmente el historial experimental de Builds,
Releases, Deployments y operaciones en vez de preservar el modelo mutable
eliminado. El único auxilio documentado es una exportación manual cifrada.
