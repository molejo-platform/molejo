# Contribuição

## Pré-requisitos

- Go 1.26.6, a versão utilizada pelo CI.
- Node.js 24 ou superior com Corepack habilitado.
- `just` 1.57 ou superior.
- Docker para os testes de integração com PostgreSQL.
- `kubectl` somente para testes de aceitação em um cluster real.

A primeira execução baixa os módulos Go e os binários do Kubernetes `envtest`.
Instale as dependências do Console com `corepack pnpm install --frozen-lockfile`.
Use `just --list` para descobrir os comandos mantidos.

## Código gerado

Edite as fontes autoritativas e execute `just generate`; não edite diretamente
os arquivos gerados:

- `contracts/molejo/clusteragent/v1alpha1/agent.proto` gera o contrato Go e gRPC
  do Agent.
- `contracts/openapi/control-plane-v1.yaml` gera os tipos do servidor Go do
  Control Plane e os tipos TypeScript do Console.
- `packages/kubernetes-api/apis/` gera o código de deep copy e `deploy/crds/`.
- As migrações e consultas do Control Plane geram
  `services/control-plane-api/internal/store/sqlc/`.

Sempre inspecione o diff gerado antes de realizar o commit.

## Testes

Execute a suíte rápida sem serviços externos:

```bash
just test
```

Os testes de integração com PostgreSQL exigem o Docker em execução. O
Testcontainers inicia o PostgreSQL 17.6 em uma porta aleatória do host e o
remove depois da suíte de cada pacote:

```bash
just integration-test
```

Para usar um PostgreSQL existente em vez do Docker, informe sua URL:

```bash
MOLEJO_TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' just integration-test
```

`just verify` executa ambas as suítes e todos os gates de qualidade do
repositório.
`just ci` também verifica que a geração e a formatação não alterem a worktree.

Durante o desenvolvimento, use primeiro o comando do menor escopo relevante:

```bash
just operator-test
just cluster-agent-test
just control-plane-test
just contract-test
just distribution-test
```

O Console também pode ser validado isoladamente:

```bash
just frontend-check
just frontend-test
just frontend-build
```

## Estratégia de testes

- Platform Operator: testes puros de renderização, depois `envtest`, usando Kind
  somente quando o comportamento exigir um cluster real.
- Cluster Agent: decisões orientadas a tabelas, TLS real com gRPC em memória e
  clientes Kubernetes simulados somente para efeitos exatos de persistência.
- Control Plane: testes puros de domínio e integrações controladas; use PostgreSQL
  real para transações, restrições, migrações, concorrência e idempotência.
- Console: verificações estáticas e testes de integração com Vitest e Testing
  Library; use Playwright somente quando o comportamento depender do navegador
  ou da integração completa entre frontend e backend.

Prefira resultados observáveis e asserções que aguardem o estado esperado a
detalhes de implementação e esperas de duração fixa. Adicione testes end-to-end
amplos somente quando o comportamento não puder ser provado em uma camada
inferior. Nunca inclua credenciais, chaves privadas, certificados ou kubeconfigs
em snapshots.

A fundação de publicação HTTP também possui uma prova local com TLS real:

```bash
tools/testing/publication-kind.sh
```

Ela cria e remove um Kind isolado com kubeconfig próprio. Requer Docker, Helm e
kubectl e nunca usa o contexto atual do cluster.

## Testes de aceitação no K3s

Estes testes para mantenedores não fazem parte do `just verify`, pois exigem um
cluster existente:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --file ./tls-setup.yaml
```

Os modos `teardown` e `cycle` do script do Control Plane modificam o cluster e
exigem o argumento explícito `--confirm <context>`. A verificação TLS é somente
leitura. Execute os scripts sem argumentos para consultar o uso completo.

## Commits

- Use mensagens no padrão Conventional Commits em inglês.
- Adicione um corpo ao commit quando a alteração envolver mais de três arquivos.
- Adicione ao stage somente os arquivos pertencentes à alteração.
