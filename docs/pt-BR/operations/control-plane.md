# Operações do Control Plane

O control plane é pre-alpha e destinado a um Workspace beta local.

## Bootstrap local

Inicie o PostgreSQL com `just db-up` e depois execute `just db-migrate`. Gere
hashes Argon2id enviando a senha por stdin para `go run
./services/control-plane-api/cmd/control-plane-api hash-password`. Configure
`FRUTO_OWNER_PASSWORD_HASH`, `FRUTO_TESTER_1_PASSWORD_HASH` e
`FRUTO_TESTER_2_PASSWORD_HASH` fora do Git e execute o comando `bootstrap`.

A API usa `KUBECONFIG` quando executada no host. No cluster, use
`FRUTO_IN_CLUSTER=true` e forneça o Secret do banco fora do repositório.

## Evidência local da Fase 6

`just control-plane-e2e-kind` cria um cluster Kind descartável e comprova as
duas topologias. A primeira executa o binário da API com `KUBECONFIG` temporário
e Vite no host, enquanto o PostgreSQL roda em Docker. A segunda executa API,
Console, Job de migration, Services e HTTPRoutes no Kind.

A suíte pinada do Playwright valida login, o ciclo completo de deployment,
histórico, deep links, sessão expirada e credenciais inválidas. O runner também
comprova a recuperação de operação pendente após restart da API, `Unknown`
enquanto o API server do Kind está pausado, idempotência por repetição e
concorrência e a ServiceAccount do control plane com `kubectl auth can-i`.
Secrets são gerados em tempo de execução e diagnósticos são coletados antes da
remoção dos recursos temporários.

## Recuperação e fronteiras

As migrations são forward-only e protegidas por serialização transacional do
PostgreSQL. Um Job de migration que falhar pode ser executado novamente depois
da inspeção dos logs. Runtime ausente ou indisponível aparece como `Unknown` e
nunca como deployment bem-sucedido. Esta instalação não declara HA ou DR.

NetworkPolicy permanece fora do gate local principal até que um CNI compatível
seja escolhido e testado explicitamente.
