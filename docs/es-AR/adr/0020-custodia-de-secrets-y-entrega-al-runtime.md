# ADR 0020: custodia de secrets y entrega al runtime

## Estado

Aceptado para la arquitectura alfa.

## Decisión

La fuente de verdad de valores es un `SecretValueStore` externo. PostgreSQL
mantiene un handle interno opaco, versión, fingerprint, metadatos y auditoría;
nunca plaintext ni paths públicos. OpenBao es el adaptador inicial, no un
requisito de producto. La API es write-only para valores y diagnósticos nunca los
contienen.

La entrega es un contrato independiente. El alfa usa
`MaterializedKubernetesSecret`: el control plane resuelve solo los valores de una
operación, los envía por el canal mTLS tipado, el Agent crea un Secret inmutable y
versionado, y el Operator recibe únicamente su referencia. Rotar crea una nueva
versión y la anterior se elimina cuando deja de estar referenciada.

El Agent recibe `get`, `create` y `delete` de Secrets solamente mediante
RoleBinding por Workspace, sin `list` o `watch`. Credenciales de providers, TLS,
Registry y componentes permanecen en namespaces dedicados. Kubernetes Secret es
materialización de última milla, no vault duradero ni fallback. El hardening de
etcd/KMS y nodes pertenece al Cluster Operator.

External Secrets Operator, Secrets Store CSI y workload identity son futuros
modos de entrega independientes del backend de almacenamiento. Cada uno declara
custodia, disponibilidad, rotación y fallas propias.

## Consecuencias

Los backends pueden sustituirse sin cambiar contratos de aplicaciones. El
control plane sigue dentro del límite de custodia en el modo alfa y una
aplicación puede divulgar cualquier secret que recibe. La entrega directa podrá
reducir esa custodia en el futuro.

## Alternativas consideradas

Kubernetes como fuente duradera, PostgreSQL como vault, ESO/CSI obligatorio y una
interfaz universal de backend y entrega fueron rechazados por acoplamiento,
custodia incompleta o dependencia de una stack opcional.

## Referencias

- [ADR 0019: provisionamiento de Workspace](0019-provisionamiento-de-workspace-y-limite-de-namespace.md)
- [Threat model de seguridad](../architecture/threat-model-de-seguridad.md)
- [Buenas prácticas de Secrets de Kubernetes](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
