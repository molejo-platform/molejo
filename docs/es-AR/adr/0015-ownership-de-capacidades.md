# ADR 0015: Ownership de capacidades

## Estado

Aceptado para la arquitectura alfa.

## Decisión

Molejo clasifica integraciones de infraestructura como `external`,
`runbook-managed`, `molejo-managed` o `provider-managed`. `molejoctl capability`
puede automatizar un runbook explícito, pero Platform Operator, Cluster Agent y
control plane no pasan a controlar el ciclo de vida de infraestructura de terceros.

Los runbooks usan entradas locales versionadas, muestran un plan antes de mutar,
marcan solamente recursos propios, rechazan adopción insegura y ofrecen verificación
read-only. Sus documentos no son CRDs ni estado persistido del producto.

Las observaciones autenticadas del Cluster Agent son hechos de runtime, no estado
de Foundation o runbooks. El control plane deriva Feature Availability de
observaciones recientes y bindings tipados sin asumir ownership externo.

El consentimiento del Cluster Operator para provisionamiento namespaced es
configuración de instalación, no autorización de actor. La custodia del backend
de secrets y la entrega al runtime son contratos tipados separados.

## Consecuencias

Los operadores pueden combinar EKS, GKE, K3s, servicios cloud administrados o
componentes OSS sin cambiar el contrato de las aplicaciones. Pueden sumarse nuevos
runbooks, pero cada integración mantiene owner y límite de ciclo de vida visibles.

## Referencias

- [ADR 0018: observación y disponibilidad](0018-observacion-de-capacidades-y-disponibilidad-de-features.md)
- [ADR 0019: provisionamiento de Workspace](0019-provisionamiento-de-workspace-y-limite-de-namespace.md)
- [ADR 0020: custodia de secrets](0020-custodia-de-secrets-y-entrega-al-runtime.md)
