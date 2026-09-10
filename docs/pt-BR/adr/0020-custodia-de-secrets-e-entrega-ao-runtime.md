# ADR 0020: custódia de secrets e entrega ao runtime

## Status

Aceito para a arquitetura alpha.

## Contexto

Um secret de aplicação cruza API pública, control plane, backend de secrets,
canal outbound do Agent, API Kubernetes e processo da aplicação. Armazenamento e
entrega possuem limites de confiança diferentes e não devem ser tratados como um
único switch de provider.

## Decisão

A fonte de verdade dos valores é um `SecretValueStore` externo. O banco do
control plane mantém handle interno opaco, versão do backend, fingerprint,
metadados de ciclo de vida e auditoria; nunca mantém plaintext nem expõe paths do
backend como IDs públicos. OpenBao é o adaptador inicial, não um requisito.

A API pública é write-only para valores. Leituras retornam metadados e estado,
nunca plaintext. Logs, métricas, traces, events, erros, idempotência, observações
e fixtures não podem conter valores.

Entrega é outro contrato. O alpha usa `MaterializedKubernetesSecret`:

1. o control plane resolve somente os valores de uma operação aceita;
2. o valor percorre o canal mTLS autenticado em payload tipado e limitado;
3. o Agent cria um Secret Kubernetes imutável, versionado e específico do
   AppEnvironment;
4. o Platform Operator recebe apenas a referência e a projeta no workload;
5. rotação cria nova versão e o valor anterior só é removido quando não estiver
   mais referenciado pelo workload reconciliado.

O Operator não recebe valores nem permissão explícita de leitura de Secrets. O
workload não recebe token Kubernetes por padrão e não seleciona Secret refs
arbitrárias pelo contrato da aplicação.

O Agent recebe `get`, `create` e `delete` apenas por RoleBinding em cada namespace
pronto; não recebe `list` ou `watch`. Credenciais de providers, TLS, Registry e
componentes da Molejo permanecem em namespaces dedicados de sistema ou capability.

Kubernetes Secret é materialização de última milha, não vault durável ou
fallback. Plaintext no PostgreSQL também é proibido. Criptografia de etcd/KMS,
nodes e hardening do cluster pertencem ao Cluster Operator; a Molejo pode
diagnosticar sua ausência sem habilitá-los silenciosamente.

Modos futuros como External Secrets Operator, Secrets Store CSI ou workload
identity são extensões do contrato de entrega, independentes do
`SecretValueStore`. Cada modo declara sua própria custódia, disponibilidade,
rotação e garantias de falha.

## Consequências

- Backends são substituíveis sem mudar contratos de parâmetros e aplicações.
- O Kubernetes recebe apenas o valor necessário ao workload, mas uma aplicação
  ainda pode divulgar um secret entregue a ela.
- Control plane e backend externo continuam limites de alto valor no modo alpha.
- Rotação é versionada e não altera um Secret compartilhado in-place.
- Entrega direta por provider pode reduzir a custódia do control plane no futuro.

## Alternativas consideradas

Kubernetes como fonte durável foi rejeitado por acoplar produto a um cluster e
ampliar leitura Kubernetes. Plaintext ou valores cifrados apenas no PostgreSQL
foram rejeitados porque a custódia da chave ainda exigiria uma raiz externa.
Obrigar ESO/CSI no alpha foi rejeitado por transformar uma stack opcional em
dependência do application loop. Uma interface universal de backend e entrega
foi rejeitada por esconder limites de confiança diferentes.

## Referências

- [ADR 0019: provisionamento de Workspace e limite de namespace](0019-provisionamento-de-workspace-e-limite-de-namespace.md)
- [Threat model de segurança da Molejo](../architecture/threat-model-de-seguranca.md)
- [Boas práticas para Secrets do Kubernetes](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
- [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
