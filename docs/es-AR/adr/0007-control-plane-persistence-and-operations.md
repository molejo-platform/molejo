# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Contexto

Aplicar intención de producto en Kubernetes cruza dos sistemas sin una
transacción distribuida. Retries, requests HTTP duplicadas y reinicios de la API
deben representarse explícitamente.

## Decisión

PostgreSQL es autoritativo para identidades, miembros del Workspace, intención de
deployment, IDs públicos, idempotencia, sesiones e historial de operaciones.
Cada mutación persiste intención y operación en una misma transacción antes del
efecto en runtime.

Las operaciones usan lease, worker ID, fencing token, versión deseada, backoff
limitado y estado `Superseded`. Delete actúa como barrera contra updates
posteriores. Kubernetes solo es autoritativo para el estado observado; runtime
indisponible o antiguo se informa como `Unknown` o `Progressing`, nunca como
`Ready` actual.

## Consecuencias

El executor puede retomar después de un crash y requests duplicadas son seguras.
El schema es forward-only en este pre-alpha y los datos locales pueden
descartarse; el único auxilio documentado es una exportación manual cifrada.
