# Releases con CI externa

La CI externa construye y publica la imagen; Molejo registra y despliega el
resultado inmutable. La integración nunca modifica Kubernetes directamente.

## Crear una credencial de automatización

Un Owner del Workspace crea un service account del App mediante
`POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/apps/{appId}/service-accounts`.
La solicitud elige los App Environments habilitados para deploy. La respuesta
muestra el token una sola vez; guardalo como secreto enmascarado de CI y nunca
lo incluyas en un commit. Para rotarlo, creá el reemplazo antes de revocar la
cuenta anterior.

## Contrato del pipeline

1. Construí y publicá la imagen OCI.
2. Resolvé el digest y registrá `repositorio@sha256:digest` con una
   `Idempotency-Key` estable para esa ejecución.
3. Leé el App Environment para obtener `version`, `configurationVersion` y
   `currentDeploymentId`.
4. Solicitá el deploy con otra clave idempotente e `If-Match` igual a la versión
   del environment.
5. Opcionalmente consultá la Operation devuelta. Un conflicto de versión indica
   que otro actor modificó el estado deseado; volvé a leer y decidí
   explícitamente si corresponde reintentar.

El control plane rechaza tags mutables y registries fuera de
`MOLEJO_ALLOWED_REGISTRIES`. Las credenciales de image pull siguen siendo una
configuración Day Zero del clúster y no viajan por esta API.

El workflow completo de referencia está en
[`docs/examples/github-actions-external-release.yml`](../../examples/github-actions-external-release.yml).
