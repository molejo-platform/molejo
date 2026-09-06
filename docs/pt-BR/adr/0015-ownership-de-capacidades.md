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

## Consequências

Operadores podem compor EKS, GKE, K3s, serviços cloud gerenciados ou componentes
OSS sem alterar o contrato das aplicações. Novos runbooks podem ser oferecidos,
mas cada integração mantém owner e limite de ciclo de vida visíveis.
