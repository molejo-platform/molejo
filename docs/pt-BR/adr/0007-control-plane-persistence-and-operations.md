# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Contexto

Aplicar intenção de produto no Kubernetes cruza dois sistemas sem transação
distribuída. Retries, requisições HTTP duplicadas e reinícios da API precisam ser
representados explicitamente.

## Decisão

PostgreSQL é autoritativo para identidades Actor, memberships de Workspace, a
hierarquia Workspace/Project/App/Environment, intenção de AppDeployment, IDs
públicos, idempotência, sessões e histórico de operações. Constraints compostas
garantem que App e Environment vinculados por um AppDeployment pertençam ao mesmo
Project e Workspace. CRUD relacional da hierarquia é síncrono e transacional;
mutações com efeito no runtime persistem intenção e operação na mesma transação
antes desse efeito.

As operações usam lease, worker ID, fencing token, versão desejada, backoff
limitado e estado `Superseded`. Delete cria uma barreira contra updates
posteriores. Kubernetes é autoritativo apenas para o estado observado; runtime
indisponível ou antigo vira `Unknown` ou `Progressing`, nunca `Ready` atual.

## Consequências

O executor pode retomar após crash e requisições duplicadas são seguras. O schema
é forward-only. AppDeployments existentes atravessam uma sequência
expand/backfill/contract com recursos de compatibilidade determinísticos; o
backfill é repetível e as constraints finais só são aplicadas quando nenhuma
relação nula permanece. Exportação manual criptografada é o único auxílio de
recuperação documentado.
