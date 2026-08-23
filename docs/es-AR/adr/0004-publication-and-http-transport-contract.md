# ADR-0004: Publication and HTTP Transport Contract

## Status

Draft

## Context

El backend stateless privado provee un Service ClusterIP estable, pero no ofrece
un endpoint público opcional. El siguiente corte vertical debe publicar el mismo
backend mediante infraestructura compartida de la plataforma sin convertir DNS,
certificados, detalles de implementación del Gateway o autorización de Kubernetes
en parte del contrato del producto. También debe preservar transportes HTTP de
larga duración y exponer un status determinístico cuando workload y publicación
cambian de forma independiente.

La unicidad del hostname no puede expresarse como una invariante local del schema
del CRD porque depende de otros objetos. El repositorio actual tampoco posee una
API del producto ni una base donde imponer una restricción transaccional de
unicidad.

## Decision

`AppDeploymentSpec` usa el enum cerrado `Private | Public`. `Private` es el default
y exige que el slug esté ausente. `Public` exige un único label DNS en minúsculas
en `spec.slug`; el hostname resultante es `{slug}.fruto.calouro.tech`.

Cada workload continúa poseyendo un Deployment y un Service ClusterIP con el mismo
nombre. Un workload público también posee un HTTPRoute con el mismo nombre en su
namespace. La ruta se conecta al listener `https` del Gateway compartido `fruto`,
en `fruto-system`, y reenvía al Service del workload. Volver a `Private` elimina
solamente el HTTPRoute controlado. El operator no crea Gateways, registros DNS ni
certificados.

Hasta que exista el control plane del producto, el operator provee ownership de
hostname determinístico y eventualmente consistente. Una ruta controlada ya
establecida se conserva; de lo contrario, timestamp de creación, nombre con
namespace y UID resuelven empates. Si ya existen rutas duplicadas, el
AppDeployment perdedor elimina solamente su propia ruta e informa
`HostnameConflict`. Preserva los campos de la release observada y vuelve a
intentar después de cinco minutos. Un futuro control plane debe reemplazar esta
frontera de asignación por una restricción atómica de unicidad, conservando el
comportamiento externo.

La disponibilidad pública exige un rollout completo del Deployment, Conditions
actuales del HTTPRoute para el parent esperado (`Accepted=True` y
`ResolvedRefs=True`) y un Gateway compartido actual. El Gateway debe informar
`Programmed=True`, y su único listener `https` debe informar `Accepted=True`,
`Programmed=True` y `ResolvedRefs=True`. Múltiples controllers informando el mismo
parent efectivo de la ruta son ambiguos y mantienen la ruta en progreso.
Conditions ausentes u obsoletas del Gateway/listener usan `GatewayProgressing`
mientras el Gateway converge. Un Gateway ausente, un Gateway/listener actual
rechazado o la ausencia de un único listener `https` después de que el Gateway
informa `Programmed=True` usa `GatewayRejected`. Una falla conocida del workload
tiene precedencia sobre el progreso de publicación. Las fallas de la ruta o del
Gateway producen Conditions públicas sanitizadas sin exponer errores técnicos.

La prueba end-to-end determinística instala Gateway API y Traefik en un cluster
Kind descartable. Usa un certificado wildcard efímero confiado por el cliente de
prueba y demuestra REST, GraphQL, SSE incremental, WebSocket persistente,
eliminación de la ruta y acceso continuo mediante el Service privado. Esta es
evidencia local de transporte, no prueba de DNS público ni de un certificado con
confianza pública. La aceptación externa de foundation permanece separada.

## Consequences

Los workloads privados y públicos comparten un único runtime e identidad de
Service. La publicación es reversible y no exige puertos en el host, acceso
directo a Pods ni mutación de DNS por el operator. REST, GraphQL, SSE y WebSocket
no necesitan objetos de ruta específicos porque usan el mismo HTTPRoute y puerto
de backend.

La asignación de hostname es segura por convergencia para el operator actual, de
una réplica y en pre-alfa, pero realiza una consulta global de AppDeployments y no
es una reserva transaccional del dominio del producto. Mayor concurrencia del
controller, múltiples réplicas y asignación a escala requieren un mecanismo de
claim indexado o perteneciente al control plane.

Gateway, listener, sufijo de dominio y política de retry son política de plataforma
sensible a compatibilidad en `v1alpha1`. Dominios customizados, autenticación,
división de tráfico y ciclo de vida de certificados permanecen fuera de esta
decisión.

## Alternatives Considered

Crear un Ingress por workload público. Esta alternativa fue rechazada porque
Gateway API provee una frontera explícita de Gateway compartido y status
estructurado de la ruta.

Exponer el Service como `LoadBalancer` o `NodePort`. Esta alternativa fue
rechazada porque evitaría la política HTTPS compartida y asignaría infraestructura
por workload.

Permitir que los usuarios provean hostnames o campos arbitrarios de HTTPRoute.
Esta alternativa fue rechazada porque expondría política de infraestructura de
Kubernetes mediante la API del producto y ampliaría prematuramente la superficie
de compatibilidad.

Exigir unicidad global transaccional dentro del operator. Esta alternativa fue
rechazada en esta fase porque las operaciones de listado y creación de Kubernetes
no proveen esa transacción en el dominio del producto. El operator converge
proyecciones duplicadas, mientras que el futuro control plane será responsable de
la asignación atómica.

Tratar TLS local exitoso como prueba de disponibilidad pública. Esta alternativa
fue rechazada porque el tráfico por port-forward en Kind no valida DNS público,
ruteo externo ni un certificado de producción con confianza pública.

## References

- [HTTPRoute de Gateway API](https://gateway-api.sigs.k8s.io/api-types/httproute/)
- [Status de Gateway API](https://gateway-api.sigs.k8s.io/guides/status/)
- [Owner references de Kubernetes](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
- [ADR-0002: Reconciliation State and Observability Contract](0002-reconciliation-state-and-observability-contract.md)
- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
