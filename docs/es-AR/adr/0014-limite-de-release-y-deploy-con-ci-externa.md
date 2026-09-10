# ADR-0014: límite de Release y deploy con CI externa

Estado: Aceptado

## Contexto

Molejo debe funcionar con GitHub Actions, pipelines de proveedores y futuros
builders dentro del clúster sin convertir ninguna opción en el modelo del
producto. Una imagen en el registry todavía no es una Release de Molejo, y
modificar Kubernetes directamente evita autorización, auditoría y conciliación.

## Decisión

La CI es responsable del checkout, build, verificaciones y push de la imagen
OCI. Después registra el artefacto inmutable `repositorio@sha256:digest` como
Release del App en el control plane. Una solicitud separada, limitada al App
Environment, selecciona esa Release para deploy. El control plane conserva la
autoridad del estado deseado; el Cluster Agent solo transporta comandos de
runtime versionados al Platform Operator.

Las automatizaciones usan un Principal ServiceAccount de primera clase. Su
credencial opaca se almacena solo como hash, vence, puede revocarse y se muestra
una vez. Los permisos son `release.write` para un App y `deployment.create`
solo para los App Environments elegidos. El deploy conserva `If-Match` e
idempotencia. La automatización puede leer sus environments permitidos y solo
las operaciones que solicitó.

El registro de Release es independiente del proveedor. Origen y procedencia son
metadatos; GitHub no es una relación obligatoria. Una clave idempotente repite el
mismo payload y entra en conflicto con otro. El mismo digest puede registrarse
en Apps o ejecuciones diferentes. Las Releases son historia inmutable y no
vencen por un contador implícito por environment.

El BuildKit administrado queda como un adaptador que genera el mismo registro de
Release. En el futuro, identidad de workload como GitHub OIDC puede reemplazar
el token opaco sin cambiar las APIs de Release y Deployment.

## Consecuencias

Los pipelines componen con Molejo mediante una API pequeña y estable. La
autenticación del registry y el image pull siguen siendo configuración de
Kubernetes/runbooks; el control plane solo aplica su allowlist. Ni el Cluster
Agent ni el Platform Operator reciben tokens de CI o código de proveedor.

Crear y revocar credenciales queda restringido inicialmente a Owners del
Workspace o Managers explícitos. La rotación consiste en crear, actualizar el
pipeline y revocar la credencial anterior.

## Alternativas consideradas

El patch directo de Kubernetes fue rechazado porque evita la fuente de verdad.
Modelar cada ejecución externa como Build fue rechazado porque acopla Release a
un proveedor. Enviar credenciales del registry al Agent u Operator fue rechazado
porque Kubernetes ya posee ese contrato.

## Referencias

- [Operación con CI externa](../application-loop/external-ci.md)
- [ADR-0013: identidad y pairing saliente del Cluster Agent](0013-outbound-cluster-agent-identity-and-pairing.md)
