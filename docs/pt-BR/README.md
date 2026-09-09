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

O repositório permanece experimental e em pre-alpha. Sua implementação atual
inclui contratos Kubernetes versionados, o Platform Operator, um backend de
control plane e o fluxo de pairing do Cluster Agent outbound. Esses componentes
não constituem uma plataforma suportada para produção nem evidência de uma release
pública suportada.

## Modelo do Produto

A hierarquia canônica é:

```text
Workspace → Project → Environment → App
```

Um `App` é a identidade lógica da aplicação. Um `AppDeployment` representa a
implantação de uma release específica de um App em um Environment.

A plataforma permanece como fonte confiável da identidade, ownership e autorização
do produto. Nomes, namespaces, labels e annotations Kubernetes são projeções de
runtime e nunca concedem permissões de produto.

## Monorepo

Este repositório é o monorepo público da Molejo. Sua estrutura atual é:

- `contracts/` — contratos versionados neutros de linguagem e gerados;
- `packages/` — bibliotecas compartilhadas com consumidores concretos;
- `services/` — Platform Operator, control plane e Cluster Agent;
- `deploy/` — artefatos de instalação Kubernetes gerados e mantidos;
- `docs/` — documentação pública de arquitetura e operações.

O código Go usa um único módulo na raiz do repositório. Novos módulos
Go e um arquivo `go.work` serão introduzidos somente quando um componente, como um
SDK público, exigir versionamento e compatibilidade de release independentes.

## Direção Tecnológica

- Go, Kubebuilder e `controller-runtime` para controllers Kubernetes;
- Go, `net/http` e Chi para APIs HTTP;
- Protocol Buffers e gRPC para o canal autenticado do Cluster Agent;
- Buildx e BuildKit para builds de containers;
- um `justfile` na raiz para comandos de desenvolvimento local.

Essas escolhas descrevem a direção inicial. Componentes são adicionados de forma
incremental e não recebem scaffold antes do início de sua fase.

## Documentação

Inglês é o idioma canônico da documentação. Versões em português (`pt-BR`) e
espanhol da Argentina (`es-AR`) são mantidas em conjunto, e outros idiomas podem
ser acrescentados futuramente.

Guias atuais de arquitetura e componentes:

- [Modelo operacional](architecture/operational-model.md)
- [Inspeção da foundation](foundation/inspect.md)
- [Ciclo de vida da plataforma](platform/lifecycle.md)
- [Platform Operator](platform/platform-operator.md)
- [Cluster Agent outbound](platform/cluster-agent.md)
- [Capacidades do cluster](capabilities/README.md)
- [Application loop](application-loop/README.md)
- [Releases por CI externa](application-loop/external-ci.md)
- [Threat model de segurança](architecture/threat-model-de-seguranca.md)
- [ADR de identidade e pairing do Cluster Agent outbound](adr/0013-outbound-cluster-agent-identity-and-pairing.md)
- [ADR do limite de Release e deploy por CI externa](adr/0014-limite-de-release-e-deploy-por-ci-externa.md)
- [ADR de ownership de capacidades](adr/0015-ownership-de-capacidades.md)
- [ADR da política de ciclo de vida alpha](adr/0016-politica-de-ciclo-de-vida-alpha.md)
- [ADR do limite da identidade humana](adr/0017-limite-da-identidade-humana.md)
- [ADR de observação de capacidades e disponibilidade](adr/0018-observacao-de-capacidades-e-disponibilidade-de-features.md)
- [ADR de provisionamento de Workspace e namespace](adr/0019-provisionamento-de-workspace-e-limite-de-namespace.md)
- [ADR de custódia de secrets e entrega](adr/0020-custodia-de-secrets-e-entrega-ao-runtime.md)
- [ADR de bindings explícitos gerenciados pelo operador](adr/0021-bindings-explicitos-gerenciados-pelo-operador.md)
- [ADR de métricas neutras com consulta Prometheus-compatible](adr/0022-metricas-neutras-com-consulta-prometheus.md)

## Contribuição

As diretrizes de contribuição serão publicadas em
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidade

- [Código de Conduta](CODE_OF_CONDUCT.md)
- [Política de Segurança](SECURITY.md)
- [Suporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
