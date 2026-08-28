# Molejo

[Início do projeto](../../README.md) | [English](../en/README.md) |
[Español (Argentina)](../es-AR/README.md)

> Projeto experimental em pre-alpha. A Molejo ainda não está pronta
> para produção.

A Molejo é uma Kubernetes Application Platform pública e portátil. Seu
objetivo é permitir que pessoas criem, publiquem e operem aplicações sem precisar
conhecer Kubernetes, `kubectl`, YAML ou a infraestrutura subjacente.

Kubernetes é o substrato de execução, não a API do produto. Usuários declaram a
intenção de produto por contratos da Molejo, e controllers confiáveis reconciliam
essa intenção em recursos Kubernetes.

## Estado

O repositório está em sua etapa de fundação. O trabalho é intencionalmente
incremental: a menor capacidade útil é implementada, observada em execução,
corrigida a partir de evidências reais e somente então ampliada.

O repositório agora contém um corte vertical executável: API de produto e Console
pre-alpha, um contrato `AppDeployment`, um operator Kubernetes, um Service
ClusterIP privado, publicação opcional por HTTPRoute e Gateway HTTPS compartilhado,
fontes de repositório por GitHub App, Builds de commit exato, Releases imutáveis
pinadas por digest e testes reproduzíveis de integração e end-to-end. O primeiro
contrato de build aceita um `Dockerfile` na raiz para `linux/amd64` por um serviço
BuildKit rootless separado. Ainda não existe automação de DNS externo nem
isolamento de workloads e builds pronto para produção.

## Modelo do Produto

A hierarquia canônica é:

```text
Workspace → Project → App + Environment
```

`App` e `Environment` são irmãos sob o mesmo Project. Um `App` é a identidade
lógica da aplicação. Um `AppEnvironment` controla branch e configuração de
runtime para um App em um Environment. Um `Deployment` é o registro imutável da
Release e revisão de configuração aplicadas nesse alvo. A revisão é criada
somente quando a configuração de runtime muda e é selecionada explicitamente
junto da Release após um preview de impacto. O `AppDeployment` Kubernetes é uma
projeção interna de runtime.

A plataforma permanece como fonte confiável da identidade, ownership e autorização
do produto. Nomes, namespaces, labels e annotations Kubernetes são projeções de
runtime e nunca concedem permissões de produto.

## Monorepo

Este repositório é o monorepo público da Molejo. Ele conterá os contratos
versionados e os componentes que implementam o produto público.

A estrutura será introduzida apenas quando cada componente possuir um consumidor
real:

- `apps/` — aplicações usadas diretamente por pessoas ou agentes de software;
- `services/` — componentes server-side executáveis de forma independente;
- `packages/` — bibliotecas reutilizáveis, contratos Kubernetes e SDKs gerados;
- `contracts/` — definições canônicas de interfaces independentes de linguagem;
- `deploy/` — artefatos de instalação Kubernetes gerados e mantidos;
- `test/e2e/` — testes que atravessam fronteiras de componentes;
- `docs/` — documentação pública de arquitetura e do projeto.

O código Go inicial usará um único módulo na raiz do repositório. Novos módulos
Go e um arquivo `go.work` serão introduzidos somente quando um componente, como um
SDK público, exigir versionamento e compatibilidade de release independentes.

## Direção Tecnológica

- Go, Kubebuilder e `controller-runtime` para controllers Kubernetes;
- Go, `net/http` e Chi para APIs HTTP;
- TypeScript, React, Vite, Tailwind CSS, shadcn/ui e Lineicons para a interface web;
- Buildx e BuildKit para builds de containers;
- um `justfile` na raiz para comandos de desenvolvimento local.

Essas escolhas descrevem a direção inicial. Componentes são adicionados de forma
incremental e não recebem scaffold antes do início de sua fase.

## Desenvolvimento

O checkout atual requer Go 1.26 ou mais recente, Node.js 24.19.0 com Corepack,
Docker com Buildx, `kubectl` e `just`. O gate local exige intencionalmente a versão
exata de Node pinada em `.node-version`. Não é necessário instalar Kind globalmente;
o comando end-to-end executa a versão pinada por meio do Go.

```bash
just generate  # regenera artefatos DeepCopy, CRD, RBAC e da API do Console
just test      # executa testes contra um API server local do envtest
just verify    # gera, verifica formatação, executa go vet e os testes
just e2e       # valida rotas privadas e públicas em um cluster Kind descartável
just ci        # executa o gate local determinístico completo
just e2e-public # valida separadamente acesso HTTPS público de saída
just frontend-check # verifica tipos da fixture React e do Console pelo lockfile pnpm
just frontend-test # executa os testes do Console e valida as duas imagens em um container restrito
just audit-frontend-images # executa a auditoria opcional com Docker Scout
```

`just ci` verifica a geração versionada, executa `just verify`, a suíte de
integração do control plane com PostgreSQL, as duas topologias do control plane
em um cluster Kind descartável e, por fim, o E2E da plataforma em Kind.

A primeira execução baixa módulos Go, binários do envtest, Kind e imagens de
container pinados. `just e2e` usa um kubeconfig temporário e não acessa o contexto
Kubernetes atualmente selecionado. Ele constrói duas versões locais da fixture
HTTP, referencia ambas por digest e valida HTTP privado e publicação HTTPS de
REST, GraphQL, SSE e WebSocket por um Gateway local com certificado efêmero
confiado pelo cliente de teste. Também valida hospedagem estática, deep links da
SPA, cache, probes, rollout, drift, self-healing e garbage collection.
`just e2e-public` adiciona somente uma chamada HTTPS real de
saída e fica fora do gate determinístico `just ci`. DNS público e certificado
publicamente confiável exigem aceite separado no ambiente da foundation.

`just audit-frontend-images` fica deliberadamente fora de `just ci`. Ele requer
Docker Scout e usa sua base mutável de vulnerabilidades para verificar achados
críticos e altos de sistema operacional e npm no builder descartado da SPA e
realiza uma verificação crítica/alta completa nas duas imagens de runtime.

O alvo manual `just e2e-frontend-k3s` é reservado a mantenedores com acesso ao
`fruto-lab`. Ele implanta imagens do registry privado por digest e mantém
`static.molejo.dev` e `spa.molejo.dev` disponíveis para inspeção.

## Operação

O [runbook do platform operator](operations/platform-operator.md) documenta o
contrato de estado, fluxo de diagnóstico, métricas protegidas e tracing opcional.
O [runbook do control plane](operations/control-plane.md) documenta o TLS local e
os fluxos autorizados de release, build e recuperação das Fases 7 e 8 no k3s.

## Documentação

Inglês é o idioma canônico da documentação. Versões em português (`pt-BR`) e
espanhol da Argentina (`es-AR`) são mantidas em conjunto, e outros idiomas podem
ser acrescentados futuramente.

Os Architecture Decision Records ficam em [`adr`](adr/README.md). Os
arquivos de ADR usam o mesmo identificador, nome de arquivo, título em inglês e
headings em inglês em todos os idiomas; somente o texto abaixo desses headings é
localizado.

## Contribuição

As diretrizes de contribuição serão publicadas em
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidade

- [Código de Conduta](CODE_OF_CONDUCT.md)
- [Política de Segurança](SECURITY.md)
- [Suporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
