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

A Console cria Workspaces, Projects, Environments e Apps exclusivamente pela API
REST autenticada. Um AppEnvironment exige App e Environment do mesmo Project e
controla branch e configuração de runtime. Cada Deployment é um snapshot
imutável de Release e configuração.
A API retorna `404` para recursos fora da membership do Actor e `403` quando um
tester tenta realizar uma mutação.

A migration 011 substitui o modelo experimental de deployment mutável por
AppEnvironments e Deployments imutáveis. Ela limpa intencionalmente o histórico
existente de Builds, Releases, Deployments e operações, preservando Actors,
Workspaces, Projects, Apps, Environments e conexões GitHub. O Job aplica as
migrations forward em ordem e nunca imprime credenciais de conexão.

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
export MOLEJO_BUILD_IMAGE_REPOSITORY='<registry>/<prefixo-de-repositorio>'

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

Depois do aceite automatizado, use o Console para conectar o App Testkit a um
Environment, configurá-lo como `Public`, construir sua branch e implantar a
Release resultante. Aguarde `Ready`, atualize o AppEnvironment, inspecione o
histórico de Deployments, reinicie `deployment/control-plane-api`, recarregue a
mesma sessão do navegador e remova o AppEnvironment. Registre somente IDs públicos, digests, estados das
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

## Parâmetros de runtime da Fase 9

Parâmetros são catalogados por Workspace como valores `PlainText` ou `Secret`
write-only. Um AppEnvironment vincula o nome de uma variável de ambiente a uma
versão imutável do parâmetro; cada Deployment captura esses vínculos e sua
`configurationVersion`. O runtime worker resolve o snapshot, armazena valores
comuns em um ConfigMap imutável, materializa secrets do OpenBao em um Secret
Kubernetes imutável e projeta no AppDeployment somente os nomes desses objetos.

Depois que a nova geração é observada como `Ready`, o worker remove objetos de
configuração antigos que tenham a annotation exata de owner do control plane, a
label managed-by, o nome determinístico e a label de versão. A remoção de um
AppEnvironment coleta todos os objetos de configuração restantes sob seu
ownership. A Role do Workspace permite que somente esse worker liste e remova
ConfigMaps e Secrets naquele Namespace; a ServiceAccount da API pública não pode
lê-los.

A instalação OpenBao incluída é uma fixture de laboratório com um nó e unseal
manual. Execute `just openbao-prepare-k3s` com o contexto aprovado `fruto-lab` e
mantenha o material de inicialização e fingerprint fora do Git. Essa fixture não
é um serviço de secrets de alta disponibilidade ou pronto para produção.

## Build plane da Fase 8

Um owner inicia um Build para um AppEnvironment cujo App possui fonte GitHub. A
API registra o commit exato da branch do AppEnvironment antes do enfileiramento.
O worker aceita somente um
`Dockerfile` na raiz, constrói `linux/amd64`, publica uma tag com o SHA do commit e
promove uma Release somente após registrar o digest OCI. Logs são limitados e
sanitizados. Um Build com falha nunca cria Release.

O build plane é instalado separadamente em `molejo-builds`. Seu daemon BuildKit
é rootless e acessível apenas pelo worker com TLS mútuo. O modo rootless upstream
para Kubernetes exige seccomp/AppArmor unconfined e
`--oci-worker-no-process-sandbox`; esta é uma fronteira pre-alpha explícita, não
uma declaração de isolamento de produção. CPU, memória, disco temporário,
concorrência de um build e timeout de 15 minutos limitam a primeira implementação.
Os dois Pods selecionam o papel de node `runtime` existente no laboratório e
`amd64`; isso mantém builds não confiáveis fora dos nodes de control plane e
dados, mas ainda compartilha um node com workloads gerenciados. Egress HTTP/HTTPS
público é permitido para dependências do Dockerfile, enquanto faixas privadas,
link-local e do cluster permanecem negadas, exceto pelos caminhos explícitos de
DNS, PostgreSQL e BuildKit.

Em checkout limpo e commitado, prepare um diretório externo de release e execute:

```bash
export FRUTO_RELEASE_DIR=/private/tmp/molejo-control-plane-release
export FRUTO_EXPECTED_CLUSTER_UID='<uid-aprovado-do-kube-system>'
export FRUTO_TRUSTED_PROXY_CIDR='<cidr-aprovado-dos-pods>'
export FRUTO_TESTKIT_IMAGE='<referencia-aprovada-do-testkit-por-digest>'
export MOLEJO_BUILD_IMAGE_REPOSITORY='<registry>/<prefixo-de-repositorio>'

just ci
just control-plane-build-release
just builds-build-release
source "$FRUTO_RELEASE_DIR/images.env"
source "$FRUTO_RELEASE_DIR/builds.env"
export FRUTO_RELEASE_OUTPUT="$FRUTO_RELEASE_DIR/control-plane.yaml"
export FRUTO_BUILDS_RELEASE_OUTPUT="$FRUTO_RELEASE_DIR/builds.yaml"
just control-plane-render-release
just builds-render-release

export GITHUB_APP_ID='<github-app-id>'
export GITHUB_APP_PRIVATE_KEY_FILE='<pem-protegido-do-github-app>'
just control-plane-prepare-k3s
just builds-prepare-k3s
just control-plane-apply-k3s
just builds-apply-k3s
```

O comando de preparação sempre usa o contexto Kubernetes `fruto-lab`, deriva uma
URL cross-namespace do Secret existente do banco do control plane, cria uma
CA privada e certificados de servidor/cliente fora do Git, monta a chave do
GitHub App somente no worker e copia a credencial existente do registry para
`molejo-builds`. A URL do banco precisa usar o Service cross-namespace
`postgres.fruto-control-plane.svc`. `MOLEJO_BUILD_DATABASE_URL_FILE` pode
sobrescrever essa origem com um arquivo protegido. Renderização, preparação dos Secrets e apply
são gates separados; não execute os comandos mutáveis sem autorização explícita
para seus recursos exatos.

O aceite exige criar um Build pelo Console, observar SHA registrado e logs
limitados, ver uma Release pinada por digest somente após sucesso e criar um
deployment a partir dela. Comprove que um Dockerfile com falha não cria Release
e que uma atualização não substitui a imagem controlada pela Release. Registre
somente IDs públicos, SHAs, digests, estados e logs sanitizados.
