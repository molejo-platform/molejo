# ADR-0013: Outbound Cluster Agent Identity and Pairing

Status: Draft

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

La conexión gRPC exige TLS 1.3 y autenticación mutua. La instalación declarada,
el URI del certificado, el fingerprint y el registro en PostgreSQL deben
coincidir. La primera versión intercambia solamente hello y heartbeat. El Agent
no tiene permisos sobre AppDeployment ni acceso general a Secrets y puede
iniciar saludable antes de que existan el control plane o el token.

## Consequences

El cluster conserva su clave privada y no acepta tráfico de administración
entrante. El control plane obtiene un transporte autenticado y versionado sin
ser dueño de credenciales de Kubernetes. El pairing pertenece a la instalación
y no es otorgado por una membresía de Workspace.

Este corte pre-alfa posee una réplica y no incluye comandos, cola, base local,
leader election, rotación automática, API de revocación, CLI ni flujo en la
Console. Los certificados vencen en siete días; la rotación debe implementarse
antes de considerar esta frontera operacionalmente durable.

## Alternatives Considered

Incorporar el conector en la API pública acopla credenciales de Kubernetes al
deployment del control plane. Los callbacks entrantes exigen exponer el cluster.
Persistir la clave privada en PostgreSQL transfiere su propiedad fuera del
cluster. Estas alternativas no fueron seleccionadas.

## References

- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Operación del Cluster Agent](../operations/cluster-agent.md)
