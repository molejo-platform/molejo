# Operación del Cluster Agent

El Cluster Agent es el único componente de Molejo que ejecuta la intención de
runtime del Control Plane en Kubernetes. Inicia un stream saliente con mTLS sobre
TLS 1.3; la API pública nunca recibe un kubeconfig y el clúster no expone un
puerto de administración entrante.

## Instalación y confianza

Instalá Operator y Agent con `molejoctl platform runtime install` y después
ejecutá `molejoctl platform control-plane install`. Una instalación nueva crea
raíces de confianza ECDSA P-256 separadas:

- `molejo-agent-ca` firma identidades cliente del Agent;
- `molejo-control-plane-server-ca` firma la identidad interna del servidor API/gRPC.

La separación impide que una clave de firma del servidor robada emita identidades
del Agent. Las instalaciones alfa anteriores que usan una sola CA no tienen una
ruta de actualización soportada; guardá los datos necesarios y reinstalá el alfa
deseado.

El ServiceAccount del Agent tiene acceso limitado a sus dos Secrets de identidad
nombrados y a los recursos Kubernetes requeridos por el contrato de runtime. No
puede listar Secrets arbitrarios. Los Secrets de configuración de runtime se
seleccionan y administran únicamente mediante nombres determinísticos derivados
de ConfigMaps pertenecientes a Molejo.

## Identidad del clúster y enrollment

Un `Cluster` es un registro durable del Control Plane. Sus credenciales son
registros hijos rotativos, no la identidad del propio Cluster. Un administrador
de la instalación crea un Cluster con `POST /api/v1/admin/clusters` y transfiere
el token devuelto, de un solo uso y válido durante diez minutos, a
`molejo-agent-enrollment` sin colocarlo en Git, el historial del shell, logs ni
chats.

El Agent persiste su clave, CSR e ID de intento antes del enrollment. Repetir el
mismo intento y CSR es idempotente. La clave privada nunca sale del clúster.
Después de validar el certificado firmado, URI SAN, raíces de confianza y
vencimiento, el Agent elimina el token de enrollment y abre su stream saliente.

Los certificados cliente duran siete días y se renuevan automáticamente durante
las últimas 24 horas. La renovación crea y persiste una clave y un CSR nuevos
antes de realizar la solicitud autenticada. La credencial anterior continúa
válida durante una superposición de una hora para que una rotación interrumpida
pueda converger de forma segura. Repetir el mismo intento de renovación devuelve
la misma credencial; el certificado nuevo y la eliminación del intento de
renovación se persisten atómicamente. Revocar un Cluster invalida todas sus
credenciales y marca como fallidas sus operaciones en cola o arrendadas.

El reemplazo de una raíz de confianza es una operación explícita de dos fases.
Antes, creá un backup cifrado de PostgreSQL y de los Secrets `molejo-agent-ca`,
`molejo-control-plane-server-ca` y `molejo-agent-server-tls`; nunca confirmes esos
backups en Git. Restaurá una CA perdida en vez de reemplazarla debajo de una
instalación activa.

Durante la transición, cada bundle contiene primero la raíz nueva y luego la
anterior, mientras que la clave activa ya pertenece a la raíz nueva. El servidor
acepta certificados cliente de cualquiera de las raíces, pero emite únicamente
desde la nueva. Conservá inicialmente el certificado servidor anterior; las
respuestas de enrollment y renovación distribuyen ambos bundles. El hello anuncia
un `trustBundleId`. Una diferencia fuerza la renovación inmediata del Agent, la
persistencia atómica del certificado, la clave y los bundles, y la confirmación
solo después de reconectar con ese material persistido.

Cambiá el certificado servidor a la CA nueva solamente después de que todos los
Clusters `Active` informen el `trustBundleId` objetivo mediante
`GET /api/v1/admin/clusters`. Eliminá las raíces antiguas únicamente después de
esa confirmación y de que transcurra la superposición de credenciales de una hora;
los Clusters sin conexión más allá del plazo operacional deben revocarse o
recuperarse explícitamente. El ID se deriva de las raíces activas y permanece
estable cuando se eliminan las raíces antiguas finales. Una ejecución idempotente
de `molejoctl platform control-plane install` continúa renovando únicamente el
certificado servidor y conserva las raíces configuradas.

## Protocolo de runtime y reconciliación

La negociación hello declara versiones y capabilities del protocolo, establece
la sesión autoritativa e informa el desfase del reloj. Los comandos incluyen una
versión de schema del payload, versión deseada, fencing token de la base de datos
y un deadline limitado por el lease de la operación.
El Agent rechaza comandos incompatibles, malformados o vencidos. El Control Plane
acepta un resultado únicamente mientras el lease PostgreSQL y el fencing token
correspondientes sigan siendo autoritativos; la memoria del proceso no forma
parte de la corrección.

Cada heartbeat puede incluir un snapshot completo de observaciones de
`AppDeployment` y `AppVolume` pertenecientes a Molejo. Contiene status, versión
deseada y un SHA-256 canónico del `spec`, nunca configuración abierta ni valores
de Secrets. El Control Plane reutiliza operaciones durables cuando falta un
objeto o cualquier parte de su `spec` diverge, incluidas réplicas, recursos,
puertos, probes, exposición y volúmenes. El Platform Operator sigue siendo
responsable de la convergencia de cada CR en Kubernetes.
Los heartbeats de sesiones reemplazadas y las secuencias repetidas o regresivas
se rechazan antes de modificar el estado observado.

El placement del Workspace es explícito mediante un Binding entre Workspace y
Cluster. Un AppEnvironment registra su Cluster objetivo, por lo que agregar un
segundo Cluster no cambia workloads existentes ni depende de un Agent global
predeterminado. La revocación conserva workloads e historial, marca los Bindings
como `Failed` y los AppEnvironments como `Unknown`. El mismo UID de Kubernetes
solo puede inscribirse en un nuevo registro Cluster después de revocar el
registro anterior.

## Salud y recuperación

`/healthz` informa la salud del proceso. `/readyz` está disponible en
`Unconfigured`, `Unpaired`, `Enrolling`, `Connecting` y `Paired`, y no está
disponible en `Initializing`, `Stopping` o `Failed`. `/status` expone el estado
sin material de identidad. Una interrupción de red vuelve a un backoff limitado.
La renovación de un certificado se reintenta con su intento persistido; un nuevo
enrollment se reserva para una identidad ausente, vencida o revocada por un
administrador.
