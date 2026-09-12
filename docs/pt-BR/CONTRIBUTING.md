# Contribuição

## Pré-requisitos

- Go 1.26.6, a versão utilizada pelo CI.
- Node.js 24 ou superior com Corepack habilitado.
- `just` 1.57 ou superior.
- `curl`, `tar` e uma ferramenta SHA-256 para instalar a versão fixada do
  ShellCheck.
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

`just lint` executa a mesma configuração do `golangci-lint` nos dois módulos
Go: o módulo do produto na raiz do repositório e o módulo separado em `tools`.
`just script-check` verifica a sintaxe Bash e executa a versão do ShellCheck
fixada no repositório e validada por checksum. A primeira execução a baixa em
`MOLEJO_TOOL_CACHE` ou em um cache temporário privado.

Use `just quality-report` para inspecionar complexidade ciclomática, tamanho de
funções e alertas de manutenibilidade em código Go que não seja de teste. Esse
relatório é diagnóstico durante o baseline alfa e não falha devido aos achados.
Os limites atuais são complexidade acima de 20, mais de 120 linhas ou 70
instruções por função e índice de manutenibilidade abaixo de 20. Não há um gate
bruto de tamanho de arquivo; avalie as funções reportadas conforme sua coesão e
responsabilidade.

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

A prova ponta a ponta mantida usa o runner de conformidade versionado:

```bash
just molejo-conformance kind
```

Ela cria e remove um Kind e um registry isolados, instala os mesmos charts do
`molejoctl` e executa `alpha-core/v1` e `http-publication/v1`. A publicação
implanta o domínio exato, adiciona um endereço do pool na mesma porta, remove o
endereço exato preservando o pool e, por fim, retira a aplicação, com Gateway e
TLS local reais. O diretório privado impresso conserva JSON e JUnit dos perfis e
do harness; artefatos de build e credenciais permanecem no scratch descartado.
Requer Docker, Helm, OpenSSL, jq e kubectl e nunca usa o contexto atual do cluster.

Targets persistentes exigem um Workspace de teste existente. O gerenciamento do
binding é aceito somente em targets descartáveis. `run` registra a identidade do
target antes dos efeitos e `cleanup --run-dir <diretório>` retoma a limpeza pelo
ledger privado. Em um target persistente, o hostname efêmero Exact pode usar um
listener wildcard informado por `--publication-exact-listener-hostname`.

O workflow dedicado executa uma vez por push, disparo manual e dia UTC. Durante
a calibração alfa ele permanece não bloqueante; deve virar obrigatório nos
caminhos cobertos somente após dez execuções qualificadas bem-sucedidas em dias
distintos.

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

O aceite da borda pública também é somente leitura e permanece separado dos
perfis de aplicação:

```bash
tools/testing/public-edge-acceptance.sh --output ./public-edge-evidence
```

Ele verifica `molejo.dev`, `cloud.molejo.dev`, o HTTP 401 com desafio Bearer do
Registry e a validade mínima dos certificados. Marcadores opcionais de corpo
confirmam a identidade do apex e do Console; a evidência conserva apenas o hash
SHA-256 do conteúdo. O script não altera DNS, Gateway, rotas ou TLS.

## Commits

- Use mensagens no padrão Conventional Commits em inglês.
- Adicione um corpo ao commit quando a alteração envolver mais de três arquivos.
- Adicione ao stage somente os arquivos pertencentes à alteração.
