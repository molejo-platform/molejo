# ADR 0015: Ownership de capacidades

## Status

Aceito para a arquitetura alpha.

## Decisão

A Molejo classifica integrações de infraestrutura como `external`,
`runbook-managed`, `molejo-managed` ou `provider-managed`. O
`molejoctl capability` pode automatizar um runbook explícito, mas Platform
Operator, Cluster Agent e control plane não passam a controlar o ciclo de vida da
infraestrutura de terceiros.

Runbooks usam entradas locais versionadas, mostram um plano antes da mutação,
marcam apenas recursos próprios, recusam adoção insegura e fornecem verificação
read-only. Seus documentos não são CRDs nem estado persistido do produto.

Observações autenticadas do Cluster Agent são fatos de runtime, não estado de
Foundation ou runbooks. O control plane combina observações recentes e bindings
tipados para derivar Feature Availability sem assumir ownership externo.

Consentimento do Cluster Operator para provisionamento namespaced é configuração
da instalação, não autorização de ator. Custódia do backend de secrets e entrega
ao runtime são contratos tipados separados.

## Consequências

Operadores podem compor EKS, GKE, K3s, serviços cloud gerenciados ou componentes
OSS sem alterar o contrato das aplicações. Novos runbooks podem ser oferecidos,
mas cada integração mantém owner e limite de ciclo de vida visíveis.

## Referências

- [ADR 0018: observação de capacidades e disponibilidade](0018-observacao-de-capacidades-e-disponibilidade-de-features.md)
- [ADR 0019: provisionamento de Workspace](0019-provisionamento-de-workspace-e-limite-de-namespace.md)
- [ADR 0020: custódia de secrets](0020-custodia-de-secrets-e-entrega-ao-runtime.md)
