# Operación del Cluster Agent

El Cluster Agent es el único componente de Molejo que ejecuta la intención de
runtime del control plane en Kubernetes. Inicia un stream saliente con mTLS y
TLS 1.3; la API pública no recibe un kubeconfig y el cluster no expone un puerto
de administración entrante.

## Instalación y confianza

Instalá Operator y Agent con `molejoctl platform runtime install` y ejecutá
`molejoctl platform control-plane install`. Una instalación nueva crea raíces ECDSA P-256
separadas: `molejo-agent-ca` firma identidades cliente de Agents y
`molejo-control-plane-server-ca` firma la identidad interna de API/gRPC. Las
instalaciones alpha anteriores conservan su CA única hasta actualizar el chart,
por lo que otra ejecución idempotente no interrumpe el Console ni el Agent.

El ServiceAccount del Agent tiene acceso limitado a sus dos Secrets de identidad
y a los recursos Kubernetes requeridos por el contrato de runtime. No puede
listar Secrets arbitrarios. Los Secrets de configuración se gestionan por nombres
determinísticos derivados de ConfigMaps pertenecientes a Molejo.

## Identidad, enrollment y rotación

Cluster es un registro durable; sus credenciales son registros hijos rotativos.
Un administrador crea el Cluster con `POST /api/v1/admin/clusters` y transfiere
el token de un solo uso, válido por diez minutos, a
`molejo-agent-enrollment`. La clave privada nunca sale del cluster y repetir el
mismo intento y CSR es idempotente.

Los certificados cliente duran siete días y se renuevan automáticamente en las
últimas 24 horas. La renovación persiste una clave, un CSR y un ID de intento
nuevos antes de la solicitud autenticada. La credencial anterior permanece
válida durante una hora para tolerar interrupciones. El certificado nuevo y la
eliminación del intento se persisten atómicamente. Revocar el Cluster invalida
todas sus credenciales y falla sus operaciones pendientes o arrendadas.

Reemplazar una raíz de confianza es una operación explícita en dos fases. Antes,
guardá un backup cifrado de PostgreSQL y de los Secrets `molejo-agent-ca`,
`molejo-control-plane-server-ca` y `molejo-agent-server-tls`, nunca en Git. Una
CA perdida se restaura; no se reemplaza debajo de una instalación activa.

Durante la transición, cada bundle contiene primero la raíz nueva y después la
anterior, y la clave activa ya corresponde a la nueva. El servidor acepta
certificados cliente de ambas raíces, pero emite sólo con la nueva. Inicialmente
se conserva el certificado servidor anterior. El hello anuncia un
`trustBundleId`; una divergencia fuerza la renovación inmediata y el Agent sólo
confirma el ID luego de persistir atómicamente y reconectar.

El certificado servidor se cambia a la CA nueva sólo cuando todos los Clusters
`Active` informan el ID objetivo en `GET /api/v1/admin/clusters`. Las raíces
anteriores se quitan únicamente después de esa confirmación y de la superposición
de una hora; los Clusters offline fuera del plazo se revocan o recuperan de forma
explícita. El ID permanece estable al quitar raíces anteriores del final del
bundle.

## Protocolo y reconciliación

El hello negocia versión y capacidades, establece la sesión autoritativa e
informa la diferencia de reloj. Cada comando contiene versión de schema, versión
deseada, fencing token persistido y deadline limitado por el lease. El Agent rechaza comandos
incompatibles, inválidos o vencidos; el control plane acepta el resultado sólo
mientras el lease y el fencing token en PostgreSQL sean válidos.

Los heartbeats pueden incluir un snapshot completo de observaciones de
`AppDeployment` y `AppVolume` pertenecientes a Molejo con estado, versión deseada
y SHA-256 canónico del `spec`, sin configuración abierta ni valores de Secrets.
El control plane reutiliza operaciones durables cuando falta un objeto o diverge
cualquier parte de su `spec`, incluidos réplicas, recursos, puertos, probes,
exposición y volúmenes. El Platform Operator continúa siendo responsable de la
convergencia Kubernetes.
Los heartbeats de sesiones reemplazadas y las secuencias repetidas o regresivas
se rechazan antes de cambiar el estado observado.

El destino es explícito mediante un vínculo Workspace-to-Cluster. Cada
AppEnvironment conserva su Cluster, por lo que agregar otro no mueve workloads
existentes ni depende de un Agent global predeterminado.
La revocación preserva workloads e historial, marca bindings como `Failed` y
AppEnvironments como `Unknown`. El mismo UID Kubernetes puede registrarse de
nuevo sólo después de revocar el registro anterior.

## Salud y recuperación

`/healthz`, `/readyz` y `/status` exponen salud y estado sin material de
identidad. Interrupciones de red vuelven a un backoff limitado. Una renovación
interrumpida reutiliza el intento persistido; un nuevo enrollment se reserva
para identidades ausentes, vencidas o revocadas.
