# ADR-0001: Fronteiras de Componentes do Monorepo e Estratégia de Módulos

## Status

Draft

## Context

A Fruto Platform conterá interfaces de usuário, ferramentas de linha de comando,
controllers Kubernetes, APIs HTTP, workers, contratos compartilhados e SDKs
gerados em várias linguagens. Colocar o primeiro código Go diretamente em
diretórios globais `api/`, `cmd/` e `internal/` simplificaria o scaffold inicial,
mas não tornaria explícitos o ownership dos componentes, suas fronteiras de
runtime ou os consumidores pretendidos à medida que a plataforma crescer.

Fronteiras de diretórios, fronteiras de módulos Go e orquestração de builds
resolvem problemas diferentes. Um componente implantável não precisa de seu
próprio módulo Go, e um `go.mod` na raiz não significa que o repositório contenha
uma única aplicação na raiz. Criar um módulo por componente antes de existir
necessidade de versionamento independente adicionaria overhead de sincronização
de dependências, testes e releases.

A Fruto também considera como ator tanto uma pessoa quanto um agente de software.
Entry points voltados a atores podem, portanto, incluir aplicações web, desktop,
TUI e CLI, além de futuros adaptadores de protocolo usados diretamente por
agentes. A localização de um futuro MCP server permanece incerta porque ele pode
se comportar tanto como um adaptador fino voltado a atores sobre APIs existentes
quanto como uma capacidade server-side da plataforma.

## Decision

O repositório organizará o código-fonte pela responsabilidade do componente:

- `apps/` contém entry points usados diretamente por atores. Atores podem ser
  pessoas ou agentes de software. Exemplos incluem aplicações web, desktop, TUI
  e CLI.
- `services/` contém componentes server-side da plataforma que executam de forma
  independente, como APIs HTTP, operators Kubernetes, controllers e workers.
- `packages/` contém bibliotecas reutilizáveis e não implantáveis e SDKs gerados
  para consumidores internos ou externos.
- `contracts/` contém definições canônicas de interfaces independentes de
  linguagem, como documentos OpenAPI, quando esses contratos existirem.
- `deploy/` contém artefatos integrados de instalação quando vários componentes
  precisarem ser instalados em conjunto.
- `test/e2e/` contém testes que atravessam fronteiras de componentes.

Diretórios serão introduzidos somente quando tiverem um componente ou consumidor
real; a árvore futura completa não será criada antecipadamente.

As dependências devem apontar para fronteiras estáveis:

- aplicações e serviços podem depender de packages e contracts;
- packages e contracts não devem depender de aplicações ou serviços;
- um serviço não deve importar a implementação de outro serviço;
- código Go privado de um componente pertence ao diretório `internal/` desse
  componente;
- a interação entre serviços ocorre por um protocolo ou contrato explícito;
- packages compartilhados são extraídos somente após existir um segundo
  consumidor real ou uma necessidade de distribuição externa.

O repositório usará inicialmente um único módulo Go na raiz:

```text
module github.com/fruto-platform/fruto
```

O código Go pode ficar sob `apps/`, `services/` e `packages/` permanecendo nesse
módulo. Os componentes podem ser compilados, testados e transformados em
containers de forma independente sem se tornarem módulos versionados de forma
independente.

Um novo `go.mod` e um `go.work` versionado na raiz serão introduzidos somente
quando um componente Go, como um SDK público, precisar de versão própria,
compatibilidade de release ou lifecycle de distribuição externa. Módulos com
release independente também devem ser testados com a resolução do workspace
desabilitada para que o `go.work` não esconda uma dependência não publicada.

O `justfile` da raiz é o runner inicial de tarefas voltado a pessoas para todas as
linguagens. Um workspace pnpm será introduzido com o primeiro componente
JavaScript ou TypeScript. Turborepo poderá ser adicionado quando múltiplos
packages JavaScript ou TypeScript produzirem um grafo real de tarefas ou uma
necessidade mensurável de cache. Bazel ou outro sistema de build poliglota exige
um problema de escala demonstrado separadamente.

A localização de um futuro MCP server não é fixada deliberadamente por esta
decisão. Um adaptador MCP fino usado diretamente por agentes e que delega para
APIs existentes da plataforma pode pertencer a `apps/`. Um componente MCP que
possui uma capacidade server-side ou um lifecycle da plataforma pertence a
`services/`. Em ambos os casos, ele não deve duplicar autorização de domínio ou
regras de negócio pertencentes ao control plane.

## Consequences

O propósito, ownership, capacidade de implantação e direção de dependências dos
componentes tornam-se visíveis pela estrutura do repositório. O primeiro contrato
Kubernetes pode ficar em `packages/kubernetes-api`, o operator pode posteriormente
ficar em `services/platform-operator`, e uma CLI para o usuário final pode
posteriormente ficar em `apps/cli`, sem colocar código de aplicação diretamente
na raiz do repositório.

Todos os packages Go iniciais compartilham uma fronteira de dependências e
release. Isso mantém simples o desenvolvimento entre componentes e o comando
`go test ./...`, mas um upgrade de dependência afeta o módulo compartilhado e os
packages Go não podem ser versionados de forma independente até serem extraídos
para outro módulo.

A estrutura depende de disciplina nas revisões: packages compartilhados
genéricos, imports diretos da implementação de serviços e diretórios vazios
prematuros enfraqueceriam as fronteiras. As ferramentas de grafo de build
permanecerão intencionalmente limitadas até que o repositório contenha componentes
suficientes para justificá-las.

A localização do MCP permanece uma decisão futura baseada no primeiro caso de uso
concreto. Isso evita tratar o nome de um protocolo como uma camada arquitetural
antes que suas responsabilidades de runtime e ownership sejam conhecidas.

## Alternatives Considered

Manter diretórios globais `api/`, `cmd/` e `internal/`. Essa alternativa segue um
layout comum de repositórios Go, mas não foi selecionada porque torna menos
explícitas as fronteiras entre componentes heterogêneos e aplicações voltadas a
atores neste monorepo.

Criar imediatamente um módulo Go para cada aplicação, serviço e package. Essa
alternativa não foi selecionada porque os componentes compartilham inicialmente o
lifecycle da release `v0.0.1`, e múltiplos módulos adicionariam sincronização de
versões e testes isolados antes de existirem releases independentes.

Restringir `apps/` a interfaces gráficas humanas. Essa alternativa não foi
selecionada porque CLI, TUI e entry points voltados a agentes também são
aplicações usadas diretamente por atores da plataforma.

Classificar agora todo MCP server como uma aplicação ou um serviço. Essa
alternativa não foi selecionada porque MCP descreve uma superfície de protocolo,
o que não é informação suficiente para determinar ownership de runtime ou
lifecycle.

Adotar Turborepo, Bazel ou outro grafo de build desde o início. Essa alternativa
não foi selecionada porque o repositório inicial não possui um grafo de tarefas ou
uma escala de build que justifique configuração e manutenção adicionais.

## References

- [Go multi-module workspaces](https://go.dev/doc/tutorial/workspaces)
- [Go module repository organization](https://go.dev/doc/modules/managing-source)
- [Turborepo package types](https://turborepo.dev/docs/core-concepts/package-types)
- [Turborepo package and task graphs](https://turborepo.dev/docs/core-concepts/package-and-task-graph)
