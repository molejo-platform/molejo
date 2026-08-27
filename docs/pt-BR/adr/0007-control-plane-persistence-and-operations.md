# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Context

Aplicar intenção de produto no Kubernetes cruza dois sistemas sem transação
distribuída. Retries, requisições HTTP duplicadas e reinícios da API precisam ser
representados explicitamente.

## Decision

PostgreSQL é autoritativo para identidades Actor, memberships de Workspace, a
hierarquia Workspace/Project/App/Environment, configuração de AppEnvironment,
histórico imutável de Deployment, IDs públicos, idempotência, sessões e histórico
de operações. Constraints compostas garantem que App e Environment vinculados por um AppEnvironment pertençam ao mesmo
Project e Workspace. CRUD relacional da hierarquia é síncrono e transacional;
mutações com efeito no runtime persistem intenção e operação na mesma transação
antes desse efeito.

As operações usam lease, worker ID, fencing token, versão desejada e backoff
limitado. Somente `EnsureWorkspace`, `ApplyDeployment` e
`DeleteAppEnvironment` cruzam a fronteira de runtime. Delete cria uma barreira contra updates
posteriores. Kubernetes é autoritativo apenas para o estado observado; runtime
indisponível ou antigo vira `Unknown` ou `Progressing`, nunca `Ready` atual.

## Consequences

O executor pode retomar após crash e requisições duplicadas são seguras. O schema
é forward-only. Como este pre-alpha não possui workloads críticos, a migration
011 descarta intencionalmente o histórico experimental de Builds, Releases,
Deployments e operações em vez de preservar o modelo mutável removido.
Exportação manual criptografada é o único auxílio de recuperação documentado.
