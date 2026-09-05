# Operación del Cluster Agent

El Cluster Agent es el único componente de Molejo que ejecuta la intención de
runtime del control plane en Kubernetes. Inicia un stream saliente con mTLS y
TLS 1.3; la API pública no recibe un kubeconfig y el cluster no expone un puerto
de administración entrante.

## Instalación y confianza

Instalá Operator y Agent con `molejoctl cluster install` y ejecutá
`molejoctl control-plane install`. Una instalación nueva crea raíces ECDSA P-256
separadas: `molejo-agent-ca` firma identidades cliente de Agents y
`molejo-control-plane-server-ca` firma la identidad interna de API/gRPC. Las
instalaciones alpha anteriores conservan su CA única hasta actualizar el chart,
por lo que otra ejecución idempotente no interrumpe el Console ni el Agent.

El ServiceAccount del Agent tiene acceso limitado a sus dos Secrets de identidad
y a los recursos Kubernetes requeridos por el contrato de runtime. No puede
listar Secrets arbitrarios.

## Identidad, enrollment y rotación

Cluster es un registro durable; sus credenciales son registros hijos rotativos.
Un administrador crea el Cluster con `POST /api/v1/admin/clusters` y transfiere
el token de un solo uso, válido por diez minutos, a
`molejo-agent-enrollment`. La clave privada nunca sale del cluster y repetir el
mismo intento y CSR es idempotente.

Los certificados cliente duran siete días y se renuevan automáticamente en las
últimas 24 horas. La renovación persiste una clave, un CSR y un ID de intento
nuevos antes de la solicitud autenticada. La credencial anterior permanece
válida durante una hora para tolerar interrupciones. Revocar el Cluster invalida
todas sus credenciales y falla sus operaciones pendientes o arrendadas.

Reemplazar la raíz de confianza no es una reparación automática. Conservá un
backup de ambos Secrets de CA. Si se pierde la CA del servidor, el instalador se
detiene y exige restaurarla en vez de desconectar silenciosamente a los Agents.
Una ejecución idempotente de `molejoctl control-plane install` renueva el
certificado del servidor cuando quedan menos de 30 días, conservando la misma CA.

## Protocolo y reconciliación

El hello negocia versión y capacidades. Cada comando contiene versión de schema,
versión deseada, fencing token persistido y deadline. El Agent rechaza comandos
incompatibles, inválidos o vencidos; el control plane acepta el resultado sólo
mientras el lease y el fencing token en PostgreSQL sean válidos.

Los heartbeats pueden incluir un snapshot completo de observaciones de
`AppDeployment` y `AppVolume` pertenecientes a Molejo, sin configuración ni
valores de Secrets. El control plane persiste el estado observado y reutiliza
`ApplyDeployment` cuando falta un objeto o su imagen difiere del estado deseado.
El Platform Operator continúa siendo responsable de la convergencia Kubernetes.

El destino es explícito mediante un vínculo Workspace-to-Cluster. Cada
AppEnvironment conserva su Cluster, por lo que agregar otro no mueve workloads
existentes ni depende de un Agent global predeterminado.

## Salud y recuperación

`/healthz`, `/readyz` y `/status` exponen salud y estado sin material de
identidad. Interrupciones de red vuelven a un backoff limitado. Una renovación
interrumpida reutiliza el intento persistido; un nuevo enrollment se reserva
para identidades ausentes, vencidas o revocadas.
