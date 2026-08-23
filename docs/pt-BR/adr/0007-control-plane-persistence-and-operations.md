# ADR-0007: Control Plane Persistence and Operations

Status: Draft

## Contexto

Aplicar intenção de produto no Kubernetes cruza dois sistemas sem transação
distribuída. Retries, requisições HTTP duplicadas e reinícios da API precisam ser
representados explicitamente.

## Decisão

PostgreSQL é autoritativo para identidades, membros do Workspace, intenção de
deployment, IDs públicos, idempotência, sessões e histórico de operações. Cada
mutação persiste intenção e operação na mesma transação antes do efeito no
runtime.

As operações usam lease, worker ID, fencing token, versão desejada, backoff
limitado e estado `Superseded`. Delete cria uma barreira contra updates
posteriores. Kubernetes é autoritativo apenas para o estado observado; runtime
indisponível ou antigo vira `Unknown` ou `Progressing`, nunca `Ready` atual.

## Consequências

O executor pode retomar após crash e requisições duplicadas são seguras. O schema
é forward-only neste pre-alpha e dados locais podem ser descartados; exportação
manual criptografada é o único auxílio de recuperação documentado.
