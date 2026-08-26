# AGENTS.md

## Identidade e objetivo

- A Molejo é uma Kubernetes Application Platform pública, portátil e
  experimental.
- Este repositório é OSS e ainda está em pre-alpha; não apresentar capacidades de
  laboratório como prontas para produção.
- Kubernetes é o substrato de execução, não a API pública do produto.
- A experiência voltada a pessoas ou agentes deve abstrair `kubectl`, YAML e
  detalhes internos da infraestrutura.
- A marca pública é Molejo, o Console usa `cloud.molejo.dev` e workloads públicos
  usam `{slug}.molejo.dev`.
- Identificadores técnicos legados que ainda contêm `fruto` exigem uma migração
  explícita; não renomeá-los como efeito colateral de mudanças de marca.

## Escopo deste repositório

- Este repositório contém as capacidades públicas do produto: contratos, CRDs,
  controllers, operators, APIs, interfaces, bibliotecas compartilhadas, SDKs e
  artefatos de instalação integrados.
- `apps/` contém entry points usados por atores, como web, CLI, TUI, desktop ou
  adapters MCP voltados ao usuário.
- `services/` contém componentes de servidor, como APIs, operators, controllers e
  workers.
- `packages/` contém bibliotecas compartilhadas com consumidor real.
- `contracts/` recebe contratos neutros de linguagem quando eles existirem.
- `deploy/` contém a instalação integrada; `test/` contém fixtures e provas locais.
- A fundação bare metal, Proxmox, redes, VMs, K3s, registry, Gateway e TLS pertence
  ao repositório privado `foundation`.
- Capacidades comerciais ou de cloud ainda não documentadas pertencem ao
  repositório separado `cloud`.
- Tratar `fruto`, `foundation` e `cloud` como repositórios Git independentes. Não
  modificar repositórios irmãos sem solicitação explícita.

## Arquitetura e contratos

- Respeitar as ADRs e a documentação atual, lembrando que ADRs em `Draft` podem
  ser ajustadas somente por uma decisão explícita, não silenciosamente durante
  uma implementação.
- Manter um único `go.mod` na raiz enquanto nenhum componente exigir versionamento
  e release independentes; não introduzir `go.work` por organização de pastas.
- Código Go privado de um componente fica no respectivo `internal/`.
- Declarar interfaces no lado consumidor e orientá-las ao caso de uso. Não criar
  repository genérico, wrapper genérico de Kubernetes ou abstração sem segundo
  comportamento comprovado.
- Separar decisão pura de domínio dos efeitos em PostgreSQL, HTTP, Kubernetes ou
  outros sistemas externos.
- Manter ownership explícito: o produto controla identidade e autorização; o
  `AppDeployment` representa intenção de runtime; o operator controla seus filhos
  e status; labels, Namespaces e nomes Kubernetes nunca autorizam operações.
- Não expor objetos, UIDs, resource versions, owner references, mensagens brutas
  ou tipos Kubernetes pela API do produto.
- Preservar idempotência, optimistic concurrency, retry retomável e comportamento
  determinístico sempre que uma mudança cruza processos ou fontes de verdade.

## Método de trabalho

- Declarar premissas, limites e critério objetivo de sucesso antes de alterar a
  base.
- Implementar o menor corte vertical que demonstre o comportamento solicitado.
- Para bugs, regressões ou novos contratos, escrever primeiro um teste que falhe
  pelo comportamento ausente; depois implementar e refatorar com a suíte verde.
- Preferir funções puras e testes table-driven para decisão de domínio; usar
  PostgreSQL real, envtest, Kind ou browser somente na fronteira que precisa ser
  comprovada.
- Cobrir caminhos de sucesso, falha, retry, idempotência, concorrência, crash
  window, ownership e sanitização quando forem relevantes ao corte.
- Preservar mudanças preexistentes no worktree e alterar somente arquivos
  diretamente relacionados ao pedido.
- Não corrigir código adjacente, atualizar dependências ou ampliar contratos sem
  necessidade demonstrada.
- Antes de implementar um manifesto solicitado, ler o arquivo inteiro e tratá-lo
  como contrato de execução; registrar divergências em vez de escolher em silêncio.

## Comandos e gates

- Usar o `justfile` como runner canônico; não adicionar um `Makefile` equivalente.
- `just generate` regenera DeepCopy, CRD e RBAC.
- `just test` executa a suíte Go com envtest.
- `just verify` executa geração, formatação, vet, testes Go e validações das
  fixtures frontend.
- `just ci` é o gate local completo e inclui o E2E Kind descartável.
- `just e2e-public` depende de egress público e permanece fora do gate
  determinístico.
- Receitas com sufixo `-k3s` são aceites manuais que podem publicar imagens ou
  alterar o cluster pessoal; executá-las somente com autorização explícita e
  contexto alvo validado.
- Depois de alterar geração, executar uma segunda geração e confirmar ausência de
  diff inesperado.
- Não promover teste unitário, envtest ou Kind a evidência de DNS, TLS, registry ou
  comportamento do k3s real.
- Manter ferramentas Go pinadas pelo `go.mod`, Node pela `.node-version` e pacotes
  frontend pelo lockfile pnpm.

## Kubernetes e operações externas

- Testes locais devem usar cluster, kubeconfig, certificados e processos
  descartáveis e limpar tudo por `trap`, inclusive após falha.
- Não alterar o contexto Kubernetes do usuário como efeito colateral de teste.
- Para qualquer operação Kubernetes deste repositório, passar explicitamente
  `--context fruto-lab` ao comando que acessa o cluster; nunca depender do
  contexto atual nem alterá-lo com `kubectl config use-context`.
- Antes de qualquer operação não descartável, resolver e informar cluster,
  contexto, Namespace, registry, hostname e recursos exatos.
- Deploy, push de imagem, mudança em DNS, Gateway, TLS, registry, cluster ou outro
  sistema remoto exigem autorização explícita para aquela etapa.
- Uma autorização de implementação local não autoriza commit, push, deploy ou
  alterações nos repositórios `foundation` e `cloud`.
- Em falha E2E ou remota, coletar diagnóstico não sensível antes da limpeza; nunca
  desabilitar TLS ou outros controles para fazer o teste passar.

## Segurança e observabilidade

- Ler `SECURITY.md` antes de ampliar autenticação, autorização, rede, secrets ou
  superfícies públicas.
- Nunca versionar ou imprimir senhas, tokens, kubeconfigs, cookies, chaves,
  connection strings, states ou dados pessoais.
- Imagens da plataforma devem usar referência imutável por digest, usuário não
  root, capabilities mínimas e filesystem somente leitura quando o runtime
  permitir.
- Logs, métricas e traces devem ser correlacionáveis, sanitizados e de baixa
  cardinalidade; IDs de recurso, Namespace, UID, digest e trace ID não são labels
  de métricas.
- Erros públicos usam códigos e reasons estáveis; detalhes técnicos permanecem em
  logs e traces sem material sensível.
- Não confundir disponibilidade do control plane com disponibilidade do runtime
  gerenciado; falha de uma dependência deve ser representada sem declarar sucesso.

## Documentação

- O README canônico da raiz é escrito em inglês e aponta para as versões
  `docs/en`, `docs/pt-BR` e `docs/es-AR`.
- Documentação pública deve preservar estrutura, caminhos, significado e
  exemplos equivalentes nos três idiomas.
- Headings e títulos de ADR permanecem em inglês em todas as traduções; somente o
  conteúdo é traduzido.
- Todas as ADRs permanecem em `Draft` até revisão explícita do usuário.
- `docs/plans/`, inclusive seu `README.md`, é planejamento local ignorado pelo Git.
  Não adicionar, forçar stage ou commitar essa árvore.
- Atualizar documentação junto da mudança que altera um contrato, mas respeitar
  pedidos explícitos para manter Markdown fora de um commit de código.

## Git e entrega

- Não executar commit, amend, push, tag, release ou deploy sem solicitação
  explícita.
- Commits usam Conventional Commits em inglês.
- Alterações em mais de três arquivos exigem corpo explicando o que mudou.
- Fazer stage seletivo e revisar os caminhos staged antes do commit.
- Nunca incluir `docs/plans/`, secrets ou alterações não relacionadas em commit.
- Após commit ou amend, validar os caminhos com `git diff-tree` e informar se não
  houve push.
- Distinguir sempre intenção no Git, validação local, aceite externo e estado
  atual observado.
