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

O Console também pode ser validado isoladamente:

```bash
just frontend-check
just frontend-test
just frontend-build
```

## Testes de aceitação no K3s

Estes testes para mantenedores não fazem parte do `just verify`, pois exigem um
cluster existente:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --profile default
```

Os modos `teardown` e `cycle` modificam o cluster e exigem o argumento explícito
`--confirm <context>`. Execute os scripts sem argumentos para consultar o uso
completo.
