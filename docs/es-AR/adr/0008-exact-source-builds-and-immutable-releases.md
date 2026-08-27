# ADR-0008: Exact Source Builds and Immutable Releases

Status: Draft

## Context

Un build ejecuta el Dockerfile no confiable de un repositorio y atraviesa GitHub,
PostgreSQL, un builder y un registry OCI. Los nombres de branch son mutables, las
credenciales no deben entrar al contexto de build y un comando exitoso sin un
digest registrado no constituye una release reproducible del producto.

## Decision

La API resuelve la branch por defecto del repositorio seleccionado a un SHA
exacto de 40 caracteres antes de crear un Build idempotente. Un worker separado
reclama Builds con lease y fencing token, obtiene un token efímero de la
instalación GitHub, descarga el archive de ese SHA exacto y lo extrae de forma
segura en un directorio descartable. El primer contrato acepta solamente un
`Dockerfile` regular en la raíz del repositorio y siempre usa `linux/amd64`.

El worker envía el contexto a un daemon BuildKit rootless separado mediante TLS
mutuo. Las credenciales de GitHub y del registry quedan montadas solamente en el
worker y nunca se copian al contexto ni se pasan por línea de comandos. Worker y
builder tienen límites explícitos de tiempo, CPU, memoria y almacenamiento
efímero. Esta topología pre-alpha ejecuta un worker y un builder; no declara
aislamiento multi-tenant fuerte.

El tag publicado es el SHA exacto del commit. Una Release se promueve de forma
transaccional solamente después de que BuildKit devuelve un digest OCI válido y
persiste imagen fijada por digest, commit, App, Build y plataforma. Los
Deployments creados desde una Release reciben la imagen de la Release en el
servidor; los clientes no pueden reemplazarla en una actualización posterior.

Buildpacks, selección de ruta en monorepos, variables y secrets de build,
webhooks, orquestación de CI, contratos de caché, SBOM, firma y scanning quedan
fuera de esta decisión.

## Consequences

La misma Release siempre identifica el mismo commit y digest, y un Build fallido
no puede volverse desplegable. Los logs de build y el estado de retry limitado
quedan disponibles sin persistir tokens GitHub. BuildKit rootless en Kubernetes
usa `--oci-worker-no-process-sandbox` y seccomp/AppArmor unconfined según el
modelo upstream; este es un riesgo pre-alpha explícito. Se requiere aislamiento
más fuerte por build antes de producción o de uso multi-tenant hostil.
