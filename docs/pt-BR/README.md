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

O escopo atual está limitado a convenções de engenharia, decisões de arquitetura e
aos primeiros contratos versionados. Ainda não existe plataforma funcional, API
pública, controller ou interface web.

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

Este repositório é o monorepo público da Molejo. Ele conterá os contratos
versionados e os componentes que implementam o produto público.

A estrutura será introduzida apenas quando cada componente possuir um consumidor
real:

- `api/` — tipos da API Kubernetes e contratos versionados;
- `cmd/` — entry points Go para controllers, APIs e outros binários;
- `internal/` — implementação Go compartilhada e privada;
- `web/` — interface web do produto;
- `config/` — artefatos de instalação Kubernetes gerados e mantidos;
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

## Documentação

Inglês é o idioma canônico da documentação. Versões em português (`pt-BR`) e
espanhol da Argentina (`es-AR`) são mantidas em conjunto, e outros idiomas podem
ser acrescentados futuramente.

Os Architecture Decision Records ficam em [`adr`](adr/README.md). Os
arquivos de ADR usam o mesmo identificador, nome de arquivo e headings em inglês
em todos os idiomas; somente o conteúdo é localizado.

## Contribuição

As diretrizes de contribuição serão publicadas em
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidade

- [Código de Conduta](CODE_OF_CONDUCT.md)
- [Política de Segurança](SECURITY.md)
- [Suporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
