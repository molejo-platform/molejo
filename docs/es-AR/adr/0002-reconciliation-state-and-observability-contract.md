# ADR-0002: Reconciliation State and Observability Contract

## Status

Draft

## Context

`AppDeployment` es un contrato asíncrono. Aplicar el Deployment deseado no
equivale a completar un rollout, y Kubernetes puede informar fallas transitorias,
persistentes o semánticas mediante mecanismos diferentes. Si esos estados se
interpretan dentro del código de I/O, la semántica del status, los retries, logs y
pruebas pueden divergir a medida que evoluciona el operator.

El diagnóstico operacional también requiere señales correlacionadas sin vincular
el operator a un backend específico de monitoreo o tracing.

## Decision

El operator evaluará una máquina de estados interna y pura a partir de un snapshot
observado. Produce exactamente un estado activo: `Ready`, `Progressing` o
`Degraded`. Un rollout estará listo solamente cuando el Deployment haya observado
su generación, todas las réplicas deseadas sean actuales y estén disponibles, no
queden réplicas antiguas y no haya réplicas no disponibles. Las Conditions de
falla del Deployment afectan la decisión solamente después de que este haya
observado su generación actual; las Conditions de una generación anterior son
obsoletas y el rollout permanece `Progressing`.

Los reasons públicos actuales forman el conjunto cerrado `DeploymentProgressing`,
`DeploymentAvailable`, `ProgressDeadlineExceeded`, `ReplicaFailure`,
`OwnershipConflict`, `ReconcileFailed`, `HTTPRouteProgressing`,
`HTTPRouteRejected`, `GatewayProgressing`, `GatewayRejected` y
`HostnameConflict`. Los reasons específicos de publicación amplían el contrato
original de estado del workload y se definen en la ADR-0004. Los conflictos de
ownership son bloqueos semánticos: actualizan el status, preservan la última
release observada con éxito y usan un requeue de cinco minutos sin retornar un
error. Los errores persistentes de la API de Kubernetes usan `ReconcileFailed` con
un mensaje sanitizado en el status, preservan los campos observados y usan el mismo
requeue limitado. Los errores transitorios de la API de Kubernetes retornan un
error y usan el backoff de controller-runtime. Las fallas informadas por el status
actual del Deployment actualizan las Conditions sin un retry artificial.

Las Conditions son el estado público duradero. Los Kubernetes Events explican
transiciones relevantes. Los logs estructurados brindan detalle técnico, las
métricas Prometheus con cardinalidad limitada brindan agregación y los spans de
OpenTelemetry brindan tiempo y causalidad. Las métricas se sirven con TLS,
autenticación y autorización de Kubernetes. La exportación OTLP es opcional y se
configura mediante variables estándar. Los detalles técnicos de los errores
permanecen en logs y traces correlacionados y no se copian al status público.

## Consequences

La semántica de estado puede probarse independientemente del I/O de Kubernetes, y
los adapters pueden cambiar sin redefinir la disponibilidad. Los reasons públicos,
los nombres de métricas y spans y la cardinalidad de labels pasan a ser contratos
sensibles a la compatibilidad.

El operator incorpora dependencias de OpenTelemetry y autenticación delegada de
Kubernetes. Las fallas de exportación de traces no deben interrumpir la
reconciliación, y los operadores deben otorgar explícitamente a los consumidores
acceso a `/metrics`.

## Alternatives Considered

Derivar la disponibilidad solamente de las réplicas disponibles. Esta alternativa
se rechazó porque un rolling update todavía puede servir réplicas antiguas.

Retornar cada estado degradado como error de reconciliación. Esta alternativa se
rechazó porque los bloqueos semánticos persistentes generarían retries rápidos y
ruidosos.

Instalar una stack de monitoreo y tracing con el operator. Esta alternativa se
rechazó porque la recolección y el almacenamiento son decisiones del despliegue y
no son necesarios para el contrato de la plataforma.

## References

- [Status de Deployment en Kubernetes](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#deployment-status)
- [Protección de métricas en Kubebuilder](https://book.kubebuilder.io/reference/metrics)
- [Exporters OpenTelemetry para Go](https://opentelemetry.io/docs/languages/go/exporters/)
- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
