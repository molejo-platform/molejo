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

## Consecuencias

Los operadores pueden combinar EKS, GKE, K3s, servicios cloud administrados o
componentes OSS sin cambiar el contrato de las aplicaciones. Pueden sumarse nuevos
runbooks, pero cada integración mantiene owner y límite de ciclo de vida visibles.
