# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Contexto

Aplicar intención de producto en Kubernetes cruza dos sistemas sin una
transacción distribuida. Retries, requests HTTP duplicadas y reinicios de la API
deben representarse explícitamente.

## Decisión

PostgreSQL es autoritativo para identidades Actor, memberships de Workspace, la
jerarquía Workspace/Project/App/Environment, intención de AppDeployment, IDs
públicos, idempotencia, sesiones e historial de operaciones. Constraints
compuestas garantizan que App y Environment vinculados por un AppDeployment
pertenezcan al mismo Project y Workspace. El CRUD relacional de la jerarquía es
síncrono y transaccional; las mutaciones con efecto en runtime persisten intención
y operación en una misma transacción antes de ese efecto.

Las operaciones usan lease, worker ID, fencing token, versión deseada, backoff
limitado y estado `Superseded`. Delete actúa como barrera contra updates
posteriores. Kubernetes solo es autoritativo para el estado observado; runtime
indisponible o antiguo se informa como `Unknown` o `Progressing`, nunca como
`Ready` actual.

## Consecuencias

El executor puede retomar después de un crash y requests duplicadas son seguras.
El schema es forward-only. Los AppDeployments existentes atraviesan una secuencia
expand/backfill/contract con recursos de compatibilidad determinísticos; el
backfill es repetible y las constraints finales solo se aplican cuando no quedan
relaciones nulas. El único auxilio documentado es una exportación manual cifrada.
