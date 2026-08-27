# Code Review Report — 2026-08-25 11:46 BRT

## 1. Resumo Executivo

O corte de hierarquia está bem orientado: o contrato é OpenAPI-first, as queries
novas usam SQLC, a autorização deriva membership no backend e as FKs compostas
impedem relações cross-Workspace/Project. A cobertura local também é relevante.

O diff, porém, ainda não deve ser considerado pronto para commit/deploy. Há dois
achados altos: as operações de arquivamento podem correr com a criação de filhos
ou deployments e quebrar a invariância de recursos ativos; e o fluxo operacional
permite executar backfill/contract antes do preflight que deveria anteceder a
mutação. Quatro achados médios e um baixo completam a revisão.

| Severidade | Quantidade |
|---|---:|
| 🔴 Crítico | 0 |
| 🟠 Alto | 2 |
| 🟡 Médio | 4 |
| 🔵 Baixo | 1 |
| **Total** | **7** |

---

## 2. Stack Técnica

| Camada | Tecnologia |
|---|---|
| Backend | Go 1.25, `net/http`, chi/oapi-codegen |
| Persistência | PostgreSQL, Goose, pgx, SQLC |
| Runtime | Kubernetes, controller-runtime, CRDs |
| Frontend | React 19, TypeScript 5.9, Vite 8, TanStack Router/Query |
| Testes | Go test/envtest/PostgreSQL, Vitest, Testing Library, Playwright estratégico |
| Contrato | OpenAPI 3.1 com geração Go e TypeScript |
| Runner | `justfile` |

O lockfile e as versões estão pinados. `pnpm audit --json` encontrou uma
vulnerabilidade alta em uma dependência de desenvolvimento, detalhada na seção
10. `govulncheck` não está disponível no toolchain atual, portanto não houve
varredura equivalente do grafo Go.

---

## 3. Serviços Externos

| Serviço | Uso ativo | Fronteira |
|---|---|---|
| PostgreSQL | Fonte de verdade de identidade, hierarquia, deployments e operations | pgx/SQLC |
| Kubernetes | Materialização de Workspace e AppDeployment | adapter de runtime |

Não foram considerados Cloudflare, GitHub ou registry como integrações ativas
deste corte: aparecem em documentação, contratos futuros ou referências OCI,
mas a hierarquia revisada não realiza chamadas diretas a esses serviços.

---

## 4. Padrões de Design

| Padrão | Avaliação |
|---|---|
| Contract-first | Bom: OpenAPI gera tipos Go e TypeScript |
| Adapter/Store | Bom: SQLC permanece na fronteira PostgreSQL |
| Operation durável | Bom para efeitos externos de Workspace/deployment |
| Optimistic concurrency | Parcial: versão é aplicada, mas reasons de conflito são colapsados |
| Keyset pagination | Bom desenho, com validação insuficiente do limite HTTP |
| Expand/backfill/contract | Bom no schema; incompleto no gate operacional |

---

## 5. Princípios de Design de Software

### 5.1 Avaliação por princípio

| Princípio | Avaliação | Observação |
|---|---|---|
| SRP | 🟡 Parcial | `AdminPage` concentra queries, mutations, estado e renderização |
| OCP | 🟢 Adequado | Não há switch crescente ou abstração prematura relevante |
| ISP/DIP | 🟢 Adequado | Handlers usam Store/casos específicos; runtime é injetado |
| DRY | 🟡 Parcial | CRUD explícito é aceitável, mas mapping de conflito perde semântica |
| KISS/YAGNI | 🟢 Adequado | Corte vertical direto, sem repository/CRUD builder genérico |

### 5.2 Violações identificadas

| # | Princípio | Arquivo | Descrição | Severidade |
|---|---|---|---|---|
| 1 | Integridade transacional | `internal/store/hierarchy.go:174` | Validação do pai e inserção do filho não compartilham lock/transação | 🟠 Alto |
| 2 | Fail-safe migration | `cmd/hierarchy-backfill/main.go:35` | `--apply` muta antes de emitir/verificar o plano | 🟠 Alto |
| 3 | Contract fidelity | `internal/api/hierarchy_handler.go:503` | Conflitos de versão e dependência têm o mesmo reason | 🟡 Médio |

---

## 6. Estrutura do Projeto

O ownership geral está coerente com o repositório:

- `contracts/openapi/` mantém a API pública;
- `services/control-plane-api/internal/domain/` contém decisões puras;
- `internal/store/` concentra PostgreSQL, migrations e SQLC;
- `internal/api/` contém handlers e autorização HTTP;
- `apps/console-web/src/features/` separa workspace, admin, deployments e operations;
- `deploy/` contém a instalação integrada;
- `test/` mantém provas de fronteira.

Os artefatos gerados representam a maior parte das linhas do diff e não revelaram
edições manuais. A geração repetida permaneceu estável.

---

## 7. Code Health

### Achado 1 — Arquivamento pode correr com criação e quebrar invariâncias

**Severidade:** 🟠 Alto

`CreateEnvironment` lê o Project e depois insere em outra operação
(`services/control-plane-api/internal/store/hierarchy.go:174-180`). Em paralelo,
`ArchiveProject` pode observar ausência de filhos e arquivar o pai
(`services/control-plane-api/internal/store/queries/control_plane.sql:114-120`).
Como a FK verifica existência, não `archived_at`, a inserção ainda pode concluir,
deixando um Project arquivado com Environment ativo. O mesmo interleaving existe
para App e entre `ResolveDeploymentHierarchy` e archive de App/Environment.

**Impacto:** quebra uma invariância central do manifesto e produz árvores que o
CRUD normal não deveria conseguir criar.

**Correção:** executar criação/archive em transações que bloqueiem o mesmo pai
(`SELECT ... FOR UPDATE` ou advisory lock determinístico por recurso). Fazer o
mesmo para App/Environment durante criação de AppDeployment e adicionar testes
PostgreSQL concorrentes que coordenem explicitamente o interleaving.

### Achado 2 — O gate de backfill pode ser ignorado pelo próprio apply

**Severidade:** 🟠 Alto

O comando declara que `--apply` aplica migrations “after reporting the plan”, mas
chama `storage.Migrate()` antes de ler `HierarchyBackfillStatus`
(`services/control-plane-api/cmd/hierarchy-backfill/main.go:15-16,35-46`). Além
disso, o Job integrado chama diretamente `control-plane-api migrate`
(`deploy/control-plane/migration-job.yaml:19-21`), executando 005–007 sem exigir o
dry-run documentado.

**Impacto:** o backfill e o `NOT NULL`/FK contract podem começar antes do
inventário, export e decisão operacional. Se 006 concluir e 007 falhar, o operador
recebe estado parcialmente avançado sem o preflight obrigatório.

**Correção:** separar APIs explícitas para expand, backfill e contract. `--apply`
deve primeiro ler/imprimir o plano, validar ambiguidades/gate de export e somente
depois executar 006, verificar zero referências nulas e executar 007. O Job de
migration precisa usar esse fluxo ou exigir uma marca durável de preflight.

### Achado 3 — `limit` do OpenAPI não é validado no handler

**Severidade:** 🟡 Médio

O contrato limita paginação a 1–100
(`contracts/openapi/control-plane-v1.yaml:554-557`), mas `hierarchyPage` apenas
converte o valor recebido (`internal/api/hierarchy_handler.go:478-492`). O router
gerado faz binding, não valida minimum/maximum. Um Actor autenticado pode pedir
um limite muito grande, ampliando consulta/alocação, ou valores negativos que
viram comportamento/erro PostgreSQL fora do contrato.

**Correção:** rejeitar fora de 1–100 antes do Store e testar `0`, `-1`, `101` e
overflow. Aplicar a mesma validação explícita a `If-Match` e comprimento de
cursor quando o router não usar middleware de validação OpenAPI.

### Achado 4 — Troca de Workspace mantém estado hierárquico do formulário

**Severidade:** 🟡 Médio

`DeploymentForm` reinicializa `draft` e `projectId` somente quando
`deployment?.id` muda (`apps/console-web/src/features/deployments/DeploymentForm.tsx:19-34`).
Ao trocar Workspace em `/deployments/new`, App/Environment do tenant anterior
permanecem no draft; se o usuário escolheu Project explicitamente, as queries do
novo Workspace continuam usando o Project antigo até nova interação.

**Impacto:** o formulário pode ficar vazio/404 ou tentar enviar referências do
tenant anterior. A API impede a troca de ownership, mas o golden path do Console
fica quebrado.

**Correção:** resetar `projectId`, `appId` e `environmentId` quando `workspaceId`
mudar. Adicionar integração React que troca Workspace com o formulário montado e
confirma novas query keys/opções — sem Playwright.

### Complexidade e performance

`AdminPage.tsx` possui mutations e renderização muito densas em um único arquivo.
Não é bloqueador neste corte, mas dificulta testes de estados específicos. Após os
achados funcionais, separar Workspace administration e Project resources em dois
componentes seria uma refatoração justificável, sem criar framework genérico.

---

## 8. Error Handling e Resiliência

### Achado 5 — Conflitos de versão e dependência são indistinguíveis

**Severidade:** 🟡 Médio

`writeHierarchyError` converte qualquer `store.ErrConflict` em
`resource_conflict` com a mensagem “changed or has active dependencies”
(`services/control-plane-api/internal/api/hierarchy_handler.go:503-512`). Assim,
stale `If-Match`, nome duplicado e archive bloqueado retornam o mesmo reason. O
Console oferece “Recarregar” para todo 409, inclusive quando a ação correta seria
remover/arquivar dependências.

**Correção:** distinguir `ErrVersionConflict`, `ErrNameConflict` e
`ErrDependencyConflict`; mapear para reasons estáveis e cobrir código + mensagem
em testes HTTP, não apenas o status 409.

Os effects Kubernetes continuam modelados por Operations duráveis, com fencing,
retry e status explícito. Esse desenho é um ponto forte do corte.

---

## 9. Observabilidade

As respostas públicas usam `requestId` e não expõem SQL. A busca não encontrou
logs de senha, cookie ou token nas mudanças revisadas. O fluxo novo, entretanto,
não adiciona eventos de auditoria específicos para create/update/archive da
hierarquia. Isso é aceitável para pre-alpha, mas deve entrar antes de administração
multiusuário real.

---

## 10. Segurança

### Achado 6 — Playwright pinado possui advisory de alta severidade

**Severidade no contexto:** 🟡 Médio (advisory upstream: alto; dependência dev)

`apps/console-web/package.json:31` fixa `@playwright/test` 1.55.0. O comando
`pnpm audit --json` reportou GHSA-7mvr-c777-76hp: versões `<1.55.1` podem baixar e
instalar browsers sem verificar corretamente a autenticidade do certificado SSL.

**Correção:** atualizar para pelo menos 1.55.1 e regenerar o lockfile, ou remover
a dependência se o smoke estratégico deixar de fazer parte do CI. Como ela roda
em ambiente de desenvolvimento/CI e não no runtime entregue, a severidade local
foi reduzida para média.

### Controles positivos observados

- membership é derivada no backend e falhas cross-Workspace retornam 404;
- mutações validam owner e CSRF;
- cookies são HttpOnly/SameSite Strict e Secure fora do perfil local explícito;
- SQL novo está parametrizado e concentrado em SQLC/Store;
- FKs compostas protegem ancestry cross-Workspace/Project;
- nenhuma credencial real foi encontrada no diff revisado.

---

## 11. Acessibilidade

### Achado 7 — Validação frontend falha silenciosamente

**Severidade:** 🔵 Baixo

`submitName` descarta a mensagem retornada por `validateAdminName`
(`apps/console-web/src/features/admin/AdminPage.tsx:58-63`) e `ResourceRow` faz o
mesmo (`AdminPage.tsx:94-97`). Um nome só com espaços passa pelo `required` nativo,
mas nenhuma mutation ocorre e nenhum erro é anunciado.

**Correção:** manter erro por formulário/campo, associá-lo com `aria-describedby`
e anunciá-lo em região `role="alert"`. Cobrir whitespace e caractere de controle
com integração React.

Labels, botões nomeados, estados de carregamento com `role="status"` e feedback
de conflito já fornecem uma base acessível adequada.

---

## 12. Matriz Consolidada de Achados

| # | Severidade | Área | Local principal | Ação |
|---|---|---|---|---|
| 1 | 🟠 Alto | Integridade | `store/hierarchy.go:174` | Serializar create/archive por recurso |
| 2 | 🟠 Alto | Migration | `cmd/hierarchy-backfill/main.go:35` | Preflight obrigatório antes de 006/007 |
| 3 | 🟡 Médio | API/performance | `api/hierarchy_handler.go:478` | Validar limites HTTP explicitamente |
| 4 | 🟡 Médio | Frontend | `DeploymentForm.tsx:19` | Resetar escopo ao trocar Workspace |
| 5 | 🟡 Médio | Contrato de erros | `api/hierarchy_handler.go:503` | Separar reasons de conflito |
| 6 | 🟡 Médio | Supply chain | `apps/console-web/package.json:31` | Atualizar/remover Playwright vulnerável |
| 7 | 🔵 Baixo | Acessibilidade | `AdminPage.tsx:58` | Renderizar erro de validação |

---

## 13. Roadmap de Remediação

### Imediato — antes de commit/deploy

1. Corrigir e testar as corridas create/archive.
2. Tornar o preflight de migration impossível de ignorar no fluxo integrado.
3. Atualizar a dependência Playwright vulnerável ou retirá-la do gate.

### Curto prazo — mesmo corte

4. Validar paginação/headers no handler.
5. Resetar o formulário quando o Workspace mudar.
6. Separar reasons de conflito e ajustar feedback do Console.

### Médio prazo

7. Exibir validação acessível por campo.
8. Adicionar auditoria estruturada de mutações administrativas.
9. Disponibilizar `govulncheck` como gate pinado do runner canônico.

---

## 14. Conclusão

**Recomendação:** solicitar mudanças antes do commit/deploy. O desenho estrutural
é sólido e os gates atuais verdes são uma boa evidência, mas não exercitam as
corridas transacionais nem garantem o gate humano do backfill. Corrigidos os dois
achados altos, os médios podem ser fechados com testes unitários, integração React
e PostgreSQL — não exigem novos cenários Playwright.
