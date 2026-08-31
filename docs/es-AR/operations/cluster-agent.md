# Operación del Cluster Agent

El Cluster Agent es un conector saliente pre-alfa. Todavía no reconcilia
workloads. El Operator puede instalarse primero; el Agent inicia como `Unpaired`,
permanece ready y espera configuración sin abrir un puerto de administración
entrante.

## Identidad de la instalación

El instalador provee `required-external-agent-ca-secret`, con `ca.crt` y
`ca.key`, y `required-external-agent-server-tls`, con `tls.crt` y `tls.key`, en
`fruto-control-plane`. El certificado de servidor debe cubrir
`control-plane-api.fruto-control-plane.svc.cluster.local`. La clave de la CA se
mantiene fuera de Git y se monta solamente en el pod de la API.

El ServiceAccount del Agent solamente puede leer y actualizar
`molejo-agent-identity` y `molejo-agent-enrollment` en `fruto-system`. No puede
listar Secrets ni acceder a los CRDs de Molejo. La clave privada es generada por
el Agent y nunca sale de `molejo-agent-identity`.

## Release en k3s

El build de la release del control plane publica el Agent como imagen
`linux/amd64` separada e inmutable. `just control-plane-prepare-k3s` crea o
reutiliza la CA ECDSA P-256 y el certificado interno de servidor, controlados
por el instalador y fuera del checkout, y aplica Secrets versionados. El render
reemplaza esos nombres de Secret y el placeholder de imagen; el apply espera el
Deployment del Agent. Protegé el directorio externo de release porque contiene
la clave privada de la CA.

## Pairing

Un administrador de la instalación llama a `POST
/api/v1/admin/agent-installations` con un nombre. La respuesta entrega una sola
vez un token de enrollment de 256 bits, válido por diez minutos. Transferilo a la
clave `token` de `molejo-agent-enrollment` sin colocarlo en Git, historial del
shell, logs o chat.

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
