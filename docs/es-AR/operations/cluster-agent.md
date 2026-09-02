# Operación del Cluster Agent

El Cluster Agent es un conector saliente pre-alfa. Todavía no reconcilia
workloads. El Operator puede instalarse primero; el Agent inicia como `Unpaired`,
permanece ready y espera configuración sin abrir un puerto de administración
entrante.

## Identidad de la instalación

`molejoctl control-plane install` crea `molejo-agent-ca`, con `ca.crt` y
`ca.key`, y `molejo-agent-server-tls`, con `tls.crt` y `tls.key`, en
`molejo-control-plane`. El certificado de servidor cubre
`control-plane-api.molejo-control-plane.svc.cluster.local`. La clave de la CA se
mantiene fuera de Git y se monta solamente en el pod de la API.

El ServiceAccount del Agent solamente puede leer y actualizar
`molejo-agent-identity` y `molejo-agent-enrollment` en `molejo-system`. No puede
listar Secrets ni acceder a los CRDs de Molejo. La clave privada es generada por
el Agent y nunca sale de `molejo-agent-identity`.

## Instalación en k3s

Instalá Operator y Agent con `molejoctl cluster install` y después ejecutá
`molejoctl control-plane install`. El segundo comando crea o reutiliza la CA
ECDSA P-256, el certificado interno, las credenciales de la base y la invitación
inicial de enrollment. Instala PostgreSQL y la API, configura los endpoints
internos HTTPS y gRPC y espera a que el Agent quede `Paired`. Las credenciales se
guardan en Secrets y solamente se imprimen con
`--show-generated-credentials`.

## Pairing

El Agent inicial en el mismo cluster se vincula automáticamente. Para Agents
adicionales, un administrador llama a `POST /api/v1/admin/agent-installations` y
transfiere el token de un solo uso a `molejo-agent-enrollment` sin colocarlo en
Git, historial del shell, logs o chat.

El Agent persiste clave, CSR e ID del intento antes del enrollment; un timeout o
crash repite la misma operación. La respuesta se valida contra la clave local,
Agent CA, vencimiento y URI de la instalación antes de persistirse. Después se
elimina el token y el Agent abre el stream mTLS con TLS 1.3.

`/healthz` representa la salud del proceso. `/readyz` permanece disponible en
`Unconfigured`, `Unpaired`, `Enrolling`, `Connecting` y `Paired`; `/status`
expone el estado sin material de identidad. `Failed` indica un Secret de
identidad ilegible, no modificable, parcial, inválido o un certificado vencido.

Este corte no tiene rotación automática. Un certificado de siete días no debe
tratarse como ciclo de vida de producción; repetir el pairing es la recuperación
temporal pre-alfa.
