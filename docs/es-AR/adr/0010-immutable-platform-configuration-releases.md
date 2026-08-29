# ADR-0010: Immutable Platform Configuration Releases

Status: Draft

## Context

Los procesos de la plataforma leen configuración y credenciales al iniciar.
Actualizar un ConfigMap o Secret con nombre fijo no cambia el template del Pod, y
los reinicios imperativos vuelven no idempotentes las aplicaciones sin cambios y
ocultan la versión activa.

## Decision

La configuración no secreta se renderiza en ConfigMaps inmutables y direccionados
por contenido, separados por consumidor. Las credenciales rotables permanecen
fuera de Git y se copian a versiones inmutables de Secret; los releases
renderizados referencian nombres exactos. Los certificados administrados pueden
proyectar un fingerprint no secreto en el template cuando su controller exige un
nombre estable.

Cada release se renderiza desde un checkout limpio y con commit y registra el
commit de origen. Un release sin cambios conserva los templates. El garbage
collection corre solamente después de la aceptación, retiene las dos versiones
más nuevas y nunca elimina una versión referenciada por Deployment, StatefulSet,
DaemonSet o Job.

## Consequences

Los cambios de configuración reinician solamente sus consumidores, el rollback
puede seleccionar una versión anterior y la rotación no divide silenciosamente
consumidores entre valores nuevos y anteriores. El material secreto no entra en
Git ni en los metadatos del release. El laboratorio todavía usa una réplica y no
declara rotación sin interrupciones ni HA.
