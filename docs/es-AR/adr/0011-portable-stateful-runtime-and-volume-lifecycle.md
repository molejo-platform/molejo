# ADR-0011: Portable Stateful Runtime and Volume Lifecycle

Status: Draft

## Context

`AppEnvironment` ya controla el branch y la configuración de runtime de una App
dentro de un Environment. Algunas aplicaciones también requieren datos que
sobrevivan a una nueva release o a la recreación del workload. Exponer PVCs,
StorageClasses, drivers CSI, nodos, zonas o identificadores de provider
convertiría la infraestructura Kubernetes en la API pública del producto y haría
que el mismo flujo variara entre clusters.

## Decision

`AppEnvironment.workloadKind` es obligatorio al crear y puede ser `Stateless` o
`Stateful`. El contrato común de update no lo modifica. La elección pertenece a
la combinación App y Environment; por eso, la misma App puede ser Stateless en un
Environment y Stateful en otro. Cada snapshot inmutable de Deployment registra
el tipo seleccionado.

El `AppDeployment` interno sigue siendo la intención estable de runtime y usa una
unión discriminada de workload. Stateless crea un Deployment y rechaza attachments
persistentes. Stateful crea un StatefulSet, exige exactamente una réplica y un
attachment ReadWriteOnce, y conserva los contratos existentes de Service,
HTTPRoute, probes, recursos, configuración, seguridad y observabilidad. Su root
filesystem permanece de solo lectura; únicamente el mount declarado es escribible.

`AppVolume` es una intención namespaced separada que pertenece al AppEnvironment.
Se proyecta en un PersistentVolumeClaim durable, pero no tiene owner reference a
una release o workload. Releases y StatefulSets lo referencian y no pueden
eliminarlo. La expansión es solo hacia arriba. La eliminación es una operación
distinta, idempotente, auditada, protegida por concurrencia optimista y rechazada
mientras un Deployment activo referencie el volumen.

Los usuarios eligen un `StorageProfile` del producto. Las capabilities públicas
contienen solamente nombre, límites de tamaño, cuota disponible, expansión,
snapshot, backup y semántica de durabilidad. La instalación vincula de forma
privada ese profile con una StorageClass. El profile inicial es
`persistent-standard`; cambiar su binding entre storage local, EBS CSI o
DigitalOcean Block Storage no modifica el dominio, contrato HTTP o flujo de la
Consola.

PostgreSQL es la fuente autoritativa de intención, versiones, reserva de cuota,
operaciones y metadatos de auditoría. Kubernetes es autoritativo únicamente para
el estado observado del runtime. Los estados y reasons públicos están
sanitizados. Nombres de claims, UIDs, storage classes, CSI handles, topología y
errores Kubernetes crudos nunca se devuelven.

## Consequences

El producto obtiene un camino stateful explícito sin duplicar su modelo de
entrega y observabilidad ni acoplarlo a un provider. El primer incremento es
pre-alpha y admite intencionalmente una réplica, un volumen, ReadWriteOnce,
retención con preservación predeterminada y expansión solo hacia arriba.

Esta decisión no ofrece conversión entre tipos de workload, volúmenes compartidos
o multi-attach, snapshots, backup, restore, importación, clonación, disponibilidad
multi-zone, failover ni garantías de producción. Los profiles node-local deben
presentarse como durabilidad local al nodo y no como alta disponibilidad.

## References

- [ADR-0002: Reconciliation State and Observability Contract](0002-reconciliation-state-and-observability-contract.md)
- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [ADR-0008: Exact Source Builds and Immutable Releases](0008-exact-source-builds-and-immutable-releases.md)
- [ADR-0010: Immutable Platform Configuration Releases](0010-immutable-platform-configuration-releases.md)
