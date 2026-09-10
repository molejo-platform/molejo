# Modelo operativo

Molejo administra aplicaciones sobre Kubernetes; no provisiona clústeres ni es
responsable por nodes, red, IAM, DNS o el control plane del proveedor. El producto
se divide en cuatro áreas operativas:

1. **Foundation** observa el sustrato Kubernetes y sus requisitos sin modificarlo.
2. **Platform lifecycle** instala y diagnostica los componentes de Molejo.
3. **Capabilities** reúne runbooks explícitos e inspeccionables que ayudan al
   operador a conectar servicios Kubernetes o de proveedores con Molejo.
4. **Application loop** cubre el camino del desarrollador entre una release
   inmutable, el estado deseado y el estado observado de la aplicación.

Este límite también define el vocabulario de la CLI: `foundation`, `platform` y
`capability`. Las operaciones del application loop permanecen en la API y la
Console; la CLI solo las expondrá cuando exista un flujo concreto del operador.

Los contratos de Molejo describen intención de producto. Las recetas de capacidades
son documentos locales, no CRDs ni una segunda fuente de verdad. Una receta debe
exponer plan, ownership, entradas, verificación e implicancias de teardown. Puede
usar una herramienta o credencial elegida por el operador, pero no debe ocultar la
creación del clúster o la administración de nodes.

## Capas de decisión de seguridad

Availability estructural, consentimiento del Cluster Operator, autorización del
actor y admission son decisiones independientes. Una observación o feature flag
puede explicar si un flujo está disponible; ninguna concede permisos. El control
plane vuelve a autorizar y admitir cada mutación.

El contrato objetivo permite al Cluster Operator habilitar provisionamiento
`Disabled` o `Namespaced` mediante la instalación del Agent. En `Namespaced`, cada
placement corresponde a un namespace de Molejo. Control plane posee el Workspace
lógico, Agent transporta estado limitado y un dominio privilegiado separado crea
namespace y RoleBindings fijos.

Storage y delivery de secrets son responsabilidades distintas. Cuando los
parámetros secretos están habilitados, un SecretValueStore externo es la fuente
duradera; Secret Kubernetes versionado es la entrega inicial, no vault ni
fallback.

La release actual es alfa. Los contratos y comandos pueden romperse entre alfas.
La transición soportada es una reinstalación experimental limpia, no un upgrade
in-place. Los controles de ownership siguen evitando adoptar o sobrescribir
recursos ajenos.

Los hechos del Agent son Capability Observations, no estado de Foundation. El
control plane combina observaciones, protocolo, estado y bindings en Feature
Availability read-only, sin mutar estado deseado. Telemetría actual e histórica
son capacidades distintas y nunca se sustituyen silenciosamente.

## Referencias de seguridad

- [Límite del producto y topología de runtime](../../en/adr/0001-product-boundary-and-runtime-topology.md)
- [Placement de Workspace y límite de privilegios de Kubernetes](../../en/adr/0004-workspace-placement-and-kubernetes-privilege-boundary.md)
- [Composición de capabilities y bindings explícitos](../../en/adr/0005-capability-composition-and-explicit-bindings.md)
- [Custodia y entrega de secrets](../../en/adr/0006-secret-custody-and-runtime-delivery.md)
- [Threat model de seguridad](threat-model-de-seguridad.md)
