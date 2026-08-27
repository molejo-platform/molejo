# ADR-0008: Exact Source Builds and Immutable Releases

Status: Draft

## Context

Um build executa o Dockerfile não confiável de um repositório e cruza GitHub,
PostgreSQL, builder e registry OCI. Nomes de branch são mutáveis, credenciais não
podem entrar no contexto de build e um comando bem-sucedido sem digest registrado
não representa uma release reproduzível do produto.

## Decision

O App seleciona um repositório, enquanto cada AppEnvironment controla a branch
usada pelo App naquele Environment. A API resolve essa branch para um SHA exato
de 40 caracteres antes de criar um Build idempotente vinculado ao AppEnvironment. Um worker separado reivindica
Builds com lease e fencing token, obtém um token efêmero da instalação GitHub,
baixa o archive daquele SHA exato e o extrai com segurança em diretório
descartável. O primeiro contrato aceita somente um `Dockerfile` regular na raiz
do repositório e sempre usa `linux/amd64`.

O worker envia o contexto para um daemon BuildKit rootless separado por TLS
mútuo. Credenciais do GitHub e do registry ficam montadas somente no worker e
nunca são copiadas para o contexto nem passadas na linha de comando. Worker e
builder possuem limites explícitos de tempo, CPU, memória e armazenamento
efêmero. Esta topologia pre-alpha executa um worker e um builder; ela não declara
isolamento multi-tenant forte.

A tag publicada é o SHA exato do commit. Uma Release é promovida
transacionalmente somente depois que o BuildKit devolve um digest OCI válido e
persiste imagem pinada por digest, commit, App, Build e plataforma. Implantar uma
Release cria um Deployment imutável com a revisão de configuração do
AppEnvironment capturada na solicitação; clientes não podem substituir sua imagem
ou configuração posteriormente.

Buildpacks, seleção de caminho em monorepo, variáveis e secrets de build,
webhooks, orquestração de CI, contratos de cache, SBOM, assinatura e scanning
permanecem fora desta decisão.

## Consequences

A mesma Release sempre identifica o mesmo commit e digest, e um Build com falha
não pode se tornar implantável. Logs de build e estado de retry limitado ficam
disponíveis sem persistir tokens GitHub. O BuildKit rootless em Kubernetes usa
`--oci-worker-no-process-sandbox` e seccomp/AppArmor unconfined conforme exigido
pelo modelo upstream; esse é um risco pre-alpha explícito. Isolamento mais forte
por build é obrigatório antes de produção ou uso multi-tenant hostil.
