# Operações do Control Plane

O control plane é pre-alpha. Um Actor pode selecionar entre os Workspaces dos
quais é membro; `owner` administra a hierarquia e `tester` possui somente leitura.

## Bootstrap local

Inicie o PostgreSQL com `just db-up` e depois execute `just db-migrate`. Gere
hashes Argon2id enviando a senha por stdin para `go run
./services/control-plane-api/cmd/control-plane-api hash-password`. Configure
`FRUTO_OWNER_PASSWORD_HASH`, `FRUTO_TESTER_1_PASSWORD_HASH` e
`FRUTO_TESTER_2_PASSWORD_HASH` fora do Git e execute o comando `bootstrap`.

A API usa `KUBECONFIG` quando executada no host e também exige
`FRUTO_EXPECTED_KUBE_CONTEXT`, `FRUTO_EXPECTED_KUBE_SERVER` e
`FRUTO_EXPECTED_CLUSTER_UID`. Ela recusa mutações quando qualquer identidade
diverge. No cluster, use `FRUTO_IN_CLUSTER=true`, forneça o UID esperado do
cluster e o Secret do banco fora do repositório.

HTTP sem TLS é aceito somente no perfil explícito de desenvolvimento em
loopback ou `*.localhost`. Para TLS local confiável pelo navegador, execute
`just dev-tls-cert` uma vez e depois `just dev-api-tls` e
`just dev-frontend-tls` em terminais separados. Certificado e chave ficam em
`.local/certs`, ignorado pelo Git, e o `mkcert` instala a CA local no trust store.
Os equivalentes HTTP são `just dev-api-http` e `just dev-frontend-http`.

A página de administração do Console cria Workspaces, Projects, Environments e
Apps exclusivamente pela API REST autenticada. A criação de AppDeployment é
escopada pelo Workspace selecionado e exige App e Environment do mesmo Project.
A API retorna `404` para recursos fora da membership do Actor e `403` quando um
tester tenta realizar uma mutação.

As migrations da hierarquia usam as versões 005 a 007: expansão, backfill
determinístico e constraints de fechamento. Execute
`just hierarchy-backfill-expand` para aplicar somente a versão 005 e depois
`just hierarchy-backfill-dry-run` para relatar exatamente os vínculos e recursos
de compatibilidade que serão criados. Use `just hierarchy-backfill-apply` somente
depois do export aprovado para aplicar as migrations forward restantes e
confirmar que nenhuma relação nula permaneceu. O Job de migration integrado usa
esse mesmo fluxo protegido: relata o plano antes da mutação, executa o backfill e
só aplica as constraints depois da verificação, seguido das migrations posteriores
em ordem de versão. Os comandos são idempotentes e
nunca imprimem credenciais de conexão.

A origem pública do Console é `https://cloud.molejo.dev`. O acesso aos
repositórios usa um GitHub App, seguindo um modelo de instalação em vez de um
OAuth App clássico. Configure como Setup URL exata
`https://cloud.molejo.dev/api/v1/github/installations/callback` e como callback
exata da autorização do usuário `https://cloud.molejo.dev/api/v1/github/callback`,
com wildcard desabilitado. Habilite apenas `Contents` de repositório como leitura;
`Metadata` permanece leitura por padrão. Não habilite webhooks, checks, escrita,
Device Flow nem autorização OAuth durante a instalação.

A API persiste a instalação do Workspace e o ID imutável do repositório, mas não
persiste tokens de usuário ou de instalação do GitHub. O token temporário do
usuário é revogado após comprovar o ownership; tokens de instalação são emitidos
sob demanda e descartados após cada request. Somente owner pode conectar,
desconectar ou alterar a fonte de um App; membros podem consultar a fonte
selecionada. Cada App possui no máximo um repositório, enquanto vários Apps podem
usar o mesmo repositório. A desconexão é rejeitada enquanto algum App ainda
referenciar a instalação.

Forneça App ID, Client ID, slug, client secret e chave privada RSA por um Secret
`molejo-github-app` aplicado fora do Git. O deployment monta as duas credenciais
como arquivos e inicia normalmente quando esse Secret opcional não existe; nesse
caso, os endpoints GitHub retornam `github_not_configured`. Use
`deploy/control-plane/github-app-secret.example.yaml` apenas como referência de
estrutura e nunca coloque credenciais reais no Git.

## Evidência local da Fase 6

`just control-plane-e2e-kind` cria um cluster Kind descartável e comprova as
duas topologias. A primeira executa o binário da API com `KUBECONFIG` temporário
e Vite no host, enquanto o PostgreSQL roda em Docker. A segunda executa API,
Console, Job de migration, Services e HTTPRoutes no Kind.

Decisões puras e clientes HTTP do frontend rodam primeiro como testes unitários
Node, sem React ou DOM. Testes de integração React/jsdom cobrem login, sessão
expirada, formulário, roteamento e estados do produto. O Playwright fica
limitado a um teste estratégico: login e ciclo completo de create, observe,
update, histórico e delete. A suíte Go com PostgreSQL comprova separadamente a
rejeição de sessões expiradas e revogadas. O runner também
comprova a recuperação de operação pendente após restart da API, `Unknown`
enquanto o API server do Kind está pausado, idempotência por repetição e
concorrência e a ServiceAccount do control plane com `kubectl auth can-i`.
Secrets são gerados em tempo de execução e diagnósticos são coletados antes da
remoção dos recursos temporários.

## Recuperação e fronteiras

As migrations são forward-only, aplicadas pelo Goose pinado e protegidas por
advisory lock de sessão do PostgreSQL. Tipos gerados pelo SQLC permanecem dentro
do adapter PostgreSQL. Um Job de migration que falhar pode ser executado novamente depois
da inspeção dos logs. Runtime ausente ou indisponível aparece como `Unknown` e
nunca como deployment bem-sucedido. Esta instalação não declara HA ou DR.

Antes do aceite no k3s, `just ci` precisa passar a partir de checkout limpo.
Registre commit de origem, `linux/amd64` e digests da API, Console e Testkit em
manifesto externo de release; substitua os placeholders de UID do cluster e
proxy confiável; crie Secrets de banco e bootstrap fora do Git; e declare se o
banco é descartável. Se não for, gere e verifique uma exportação manual
criptografada antes do rollout. Rollback significa reaplicar digests compatíveis;
migration forward desconhecida pelo binário anterior bloqueia rollback. A
recuperação é restauração manual em PostgreSQL separado, seguida por
`SchemaReady` e teste funcional. Isso não é disaster recovery de produção.

Execute o preflight read-only `just control-plane-preflight-k3s` com contexto,
servidor, UID do cluster e digests aprovados. Depois do sucesso, use
`just control-plane-render-release` para renderizar o Kustomize pinado por digest
em caminho absoluto fora do checkout. Nenhum dos dois comandos aplica recursos;
a mutação do cluster continua sendo uma etapa manual com autorização separada.

## Aceite k3s da Fase 7

Use um checkout limpo e commitado e um diretório externo com modo `0700`. O
overlay de laboratório pina o PostgreSQL 17.6 pelo digest do manifesto
`linux/amd64`, agenda-o em `fruto-data-01`, solicita um volume `local-path` de
2 GiB e marca serviço e storage como fixtures pre-alpha descartáveis. Isso não é
HA nem banco gerenciado.

```bash
export FRUTO_RELEASE_DIR=/private/tmp/molejo-control-plane-release
export FRUTO_K3S_CONTEXT=fruto-lab
export FRUTO_EXPECTED_KUBE_SERVER='<approved-kube-api-url>'
export FRUTO_EXPECTED_CLUSTER_UID='<approved-kube-system-uid>'
export FRUTO_TRUSTED_PROXY_CIDR='<approved-pod-cidr>'
export FRUTO_TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit@sha256:1b5a36a776cc16dd3fa728c2269109ca45fca2a4af622b3a166e4e578b9cdb08

just ci
just control-plane-build-release
source "$FRUTO_RELEASE_DIR/images.env"
just control-plane-preflight-k3s
export FRUTO_RELEASE_OUTPUT="$FRUTO_RELEASE_DIR/control-plane.yaml"
just control-plane-render-release
just control-plane-prepare-k3s
just control-plane-apply-k3s
just control-plane-accept-k3s
```

A preparação gera a senha do owner, seu hash Argon2id e as credenciais do
PostgreSQL em `$FRUTO_RELEASE_DIR/secrets`, com modo `0600`; somente referências
a Secrets chegam aos PodSpecs. Ela também copia a credencial de pull do registry
para `fruto-control-plane` sem gravá-la no repositório ou terminal. Recupere a
senha do owner localmente para o aceite no navegador e rotacione-a depois do
teste. Não cole senhas, hashes, URLs de banco, dados de Secret ou kubeconfigs em
issues, logs, commits ou chat.

Depois do aceite automatizado, use o Console para criar como `Public` o digest
registrado do Testkit, aguarde `Ready`, atualize, inspecione o histórico,
reinicie `deployment/control-plane-api`, recarregue a mesma sessão do navegador
e remova o deployment. Registre somente IDs públicos, digests, estados das
operações, condições das rotas e contagens. Uma segunda aplicação do mesmo
bundle é a prova de rollback da primeira release; outro digest anterior só pode
ser reaplicado quando seu binário compreender todas as migrations forward já
aplicadas.

Como o banco deste laboratório é explicitamente descartável, recuperação
significa recriar a fixture, reaplicar migrations e bootstrap e comprovar
`SchemaReady` e o fluxo funcional. Se a instalação passar a não descartável,
pare e prove exportação manual criptografada e restauração em outro PostgreSQL
antes do rollout. Nenhum caminho declara disaster recovery de produção.

Os máximos de réplicas, CPU e memória são quotas de produto aplicadas pela API
pública. O CRD aplica intencionalmente apenas a validade de runtime e as relações
entre requests e limits; ele não duplica essas quotas de produto.

A NetworkPolicy do repositório é um exemplo incompleto e não instalado. O egress
para PostgreSQL não restringe destino porque a instalação portátil ainda não
possui um contrato de destino do banco. Não a instale como está: primeiro defina
o destino do banco e o CNI e depois valide a política resultante.
