# ADR-0013: Outbound Cluster Agent Identity and Pairing

Status: Accepted

## Context

El Operator reconcilia la intención de runtime sin quedar expuesto a un control
plane remoto. Una instalación portable todavía necesita una conexión acotada que
atraviese los límites del cluster y de la red sin entregar una credencial de
Kubernetes a la API pública ni requerir un puerto de administración entrante.

## Decision

El Cluster Agent posee una identidad de instalación e inicia un stream gRPC
saliente hacia el control plane. Genera una clave ECDSA P-256 dentro del cluster,
la persiste en un único Secret nombrado, se inscribe con un token de un solo uso
válido por diez minutos y envía solamente un CSR. Una Agent CA provista por el
instalador firma un certificado de cliente válido por siete días cuyo URI SAN es
`spiffe://molejo.dev/agent/{installationId}`.

La conexión gRPC exige TLS 1.3 y autenticación mutua. La identidad durable del
Cluster y sus credenciales rotativas son registros separados. Cluster, URI,
fingerprint y registro en PostgreSQL deben coincidir. Las instalaciones nuevas
usan raíces separadas para cliente y servidor. Los certificados de siete días se
renuevan automáticamente con un intento idempotente persistido y una
superposición de una hora. Un administrador puede revocar el Cluster y todas sus
credenciales.

El protocolo versionado negocia capacidades y transporta comandos con versión de
schema, deadline, lease durable y fencing token. El Agent informa solamente
observaciones no sensibles de objetos Molejo. Estado deseado, routing, auditoría
y operaciones permanecen autoritativos en PostgreSQL. Los vínculos
Workspace-to-Cluster y el destino del AppEnvironment son explícitos.

## Consequences

El cluster conserva su clave privada y no acepta tráfico de administración
entrante. El control plane obtiene un transporte autenticado y versionado sin
ser dueño de credenciales de Kubernetes. El pairing pertenece a la instalación
y no es otorgado por una membresía de Workspace.

El control plane puede reiniciarse o ejecutar varias réplicas sin perder el
fencing porque el comando activo no reside en memoria del proceso. Durante la
rotación el Agent puede reconectarse con la credencial anterior, pero sólo una
operación viva se arrienda por Cluster. Agregar un Cluster no mueve workloads de
forma implícita. La reconciliación Kubernetes sigue perteneciendo al Platform
Operator; el Agent aplica intención contratada e informa observación.

## Alternatives Considered

Incorporar el conector en la API pública acopla credenciales de Kubernetes al
deployment del control plane. Los callbacks entrantes exigen exponer el cluster.
Persistir la clave privada en PostgreSQL transfiere su propiedad fuera del
cluster. Estas alternativas no fueron seleccionadas.

## References

- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Operación del Cluster Agent](../platform/cluster-agent.md)
