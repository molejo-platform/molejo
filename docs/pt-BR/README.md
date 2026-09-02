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

Guias atuais dos componentes:

- [Platform Operator](operations/platform-operator.md)
- [Cluster Agent outbound](operations/cluster-agent.md)
- [TLS do cluster](operations/tls.md)
- [Setup day zero no K3s](operations/cluster-setup.md)
- [Acesso ao registry de aplicações](operations/registry-access.md)
- [ADR de identidade e pairing do Cluster Agent outbound](adr/0013-outbound-cluster-agent-identity-and-pairing.md)

## Contribuição

As diretrizes de contribuição serão publicadas em
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidade

- [Código de Conduta](CODE_OF_CONDUCT.md)
- [Política de Segurança](SECURITY.md)
- [Suporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
