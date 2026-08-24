# ADR-0005: Frontend Image Runtime Contract

## Status

Draft

## Context

La plataforma debe comprobar que sitios estáticos y SPAs ejecutadas en el
navegador pueden usar el mismo ciclo de vida de `AppDeployment` ya utilizado por
backends HTTP. Codificar un framework o herramienta de build en la API de
Kubernetes acoplaría la reconciliación de runtime a decisiones de build y
ampliaría el contrato público sin necesidad.

Las imágenes también deben funcionar con el runtime restringido: non-root, root
filesystem de solo lectura, sin capabilities, referencias OCI inmutables y
probes HTTP fijas.

## Decision

`static-html` y `vite-react-spa` son contratos de imagen de referencia mantenidos
por la plataforma, no valores de `AppDeploymentSpec`. El operator acepta cualquier
imagen HTTP inmutable que satisfaga el puerto, las probes, los resources y el
runtime restringido declarados; no inspecciona el framework de frontend ni el
servidor HTTP dentro de la imagen. Ambos perfiles de referencia exponen HTTP en el
puerto `8080`, ofrecen `/healthz` y `/readyz` y ejecutan NGINX controlado por el
repositorio como UID/GID `65532:65532`. Los temporales permanecen en `/dev/shm`.

El perfil estático sirve documentos HTML independientes y responde `404` para
paths desconocidos. El perfil SPA sirve artefactos del build Vite y devuelve
`index.html` con HTTP `200` para rutas del navegador desconocidas por NGINX. El
router del cliente decide si renderiza una ruta o una página Not Found. Los assets
ausentes devuelven `404` y nunca reciben el shell de la SPA. HTML usa
`Cache-Control: no-cache` y se revalida; los assets con fingerprint usan cache
inmutable por un año.

El repositorio contiene fixtures mínimas para ambos contratos. La fixture Vite
es un consumidor del workspace pnpm usado para comprobar build y runtime; no es
la aplicación web del producto Molejo. Comprueba el fallback del servidor, pero no
proporciona una página Not Found específica de un router. Las imágenes se
despliegan solo por digest.

## Consequences

Los sitios estáticos y las SPAs reutilizan Deployment, Service, HTTPRoute, status,
rollout, corrección de drift y garbage collection sin modificar el CRD ni el
controller. La política NGINX permanece revisable y testeable por la plataforma.

Las aplicaciones que usan estos perfiles de referencia no pueden inyectar
directivas NGINX arbitrarias mediante `AppDeployment`. Los desarrolladores siguen
siendo libres de publicar una imagen HTTP propia con otro servidor o política de
rutas. SSR, builds desde Git, CDN, detección de framework y selección de perfil por
el usuario en la API de Kubernetes quedan fuera de esta decisión.

## Alternatives Considered

Agregar `spec.profile` a `AppDeployment`. Rechazado porque el operator solo
necesita un contrato de imagen HTTP válido y no debe conocer cómo se construyeron
los assets.

Permitir que cada aplicación inyecte configuración NGINX en los perfiles de
referencia mantenidos mediante `AppDeployment`. Rechazado porque ampliaría la API
pública y la superficie de seguridad y compatibilidad antes de un consumidor
real. Una imagen totalmente propia sigue siendo compatible.

Usar una política de fallback para todos los archivos. Rechazado porque devolver
`index.html` para JavaScript o CSS ausente oculta fallas y rompe el cache.

## References

- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
- [Guía de deployment estático de Vite](https://vite.dev/guide/static-deploy.html)
- [Módulo de headers de NGINX](https://nginx.org/en/docs/http/ngx_http_headers_module.html)
