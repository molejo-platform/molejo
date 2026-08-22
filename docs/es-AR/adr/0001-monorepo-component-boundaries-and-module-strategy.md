# ADR-0001: Límites de Componentes del Monorepo y Estrategia de Módulos

## Status

Draft

## Context

Fruto Platform contendrá interfaces de usuario, herramientas de línea de comandos,
controllers de Kubernetes, APIs HTTP, workers, contratos compartidos y SDKs
generados en varios lenguajes. Colocar el primer código Go directamente en
directorios globales `api/`, `cmd/` e `internal/` simplificaría el scaffold
inicial, pero no haría explícitos el ownership de los componentes, sus límites de
runtime ni los consumidores previstos a medida que la plataforma crezca.

Los límites de directorios, los límites de módulos Go y la orquestación de builds
resuelven problemas diferentes. Un componente desplegable no necesita su propio
módulo Go, y un `go.mod` en la raíz no implica que el repositorio contenga una
única aplicación en la raíz. Crear un módulo por componente antes de necesitar
versionado independiente agregaría overhead de sincronización de dependencias,
pruebas y releases.

Fruto también considera actor tanto a una persona como a un agente de software.
Por lo tanto, los entry points orientados a actores pueden incluir aplicaciones
web, desktop, TUI y CLI, además de futuros adaptadores de protocolo usados
directamente por agentes. La ubicación de un futuro MCP server permanece incierta
porque puede comportarse como un adaptador delgado orientado a actores sobre APIs
existentes o como una capacidad server-side de la plataforma.

## Decision

El repositorio organizará el código fuente según la responsabilidad del
componente:

- `apps/` contiene entry points usados directamente por actores. Los actores
  pueden ser personas o agentes de software. Algunos ejemplos son aplicaciones
  web, desktop, TUI y CLI.
- `services/` contiene componentes server-side de la plataforma que se ejecutan
  de manera independiente, como APIs HTTP, operators de Kubernetes, controllers y
  workers.
- `packages/` contiene bibliotecas reutilizables y no desplegables y SDKs
  generados para consumidores internos o externos.
- `contracts/` contiene definiciones canónicas de interfaces independientes del
  lenguaje, como documentos OpenAPI, cuando esos contratos existan.
- `deploy/` contiene artefactos integrados de instalación cuando sea necesario
  instalar varios componentes juntos.
- `test/e2e/` contiene pruebas que atraviesan límites de componentes.

Los directorios se incorporarán solamente cuando tengan un componente o
consumidor real; no se creará anticipadamente el árbol futuro completo.

Las dependencias deben apuntar hacia límites estables:

- las aplicaciones y los servicios pueden depender de packages y contracts;
- packages y contracts no deben depender de aplicaciones ni servicios;
- un servicio no debe importar la implementación de otro servicio;
- el código Go privado de un componente pertenece al directorio `internal/` de
  ese componente;
- la interacción entre servicios ocurre mediante un protocolo o contrato
  explícito;
- los packages compartidos se extraen solamente después de que exista un segundo
  consumidor real o una necesidad de distribución externa.

El repositorio usará inicialmente un único módulo Go en la raíz:

```text
module github.com/fruto-platform/fruto
```

El código Go puede estar bajo `apps/`, `services/` y `packages/` mientras
permanece en ese módulo. Los componentes pueden compilarse, probarse y convertirse
en containers de manera independiente sin convertirse en módulos versionados de
forma independiente.

Un nuevo `go.mod` y un `go.work` versionado en la raíz se incorporarán solamente
cuando un componente Go, como un SDK público, necesite su propia versión,
compatibilidad de release o lifecycle de distribución externa. Los módulos con
release independiente también deben probarse con la resolución del workspace
deshabilitada para que `go.work` no oculte una dependencia no publicada.

El `justfile` de la raíz es el runner inicial de tareas orientado a personas para
todos los lenguajes. Se incorporará un workspace pnpm con el primer componente
JavaScript o TypeScript. Turborepo podrá agregarse cuando múltiples packages
JavaScript o TypeScript produzcan un grafo real de tareas o una necesidad medible
de caché. Bazel u otro sistema de build políglota requiere un problema de escala
demostrado por separado.

La ubicación de un futuro MCP server no queda fijada deliberadamente por esta
decisión. Un adaptador MCP delgado usado directamente por agentes y que delega en
APIs existentes de la plataforma puede pertenecer a `apps/`. Un componente MCP
que posee una capacidad server-side o un lifecycle de la plataforma pertenece a
`services/`. En ambos casos, no debe duplicar autorización de dominio ni reglas de
negocio pertenecientes al control plane.

## Consequences

El propósito, ownership, capacidad de despliegue y dirección de dependencias de
los componentes se vuelven visibles en la estructura del repositorio. El primer
contrato de Kubernetes puede estar en `packages/kubernetes-api`, el operator puede
estar posteriormente en `services/platform-operator`, y una CLI para el usuario
final puede estar posteriormente en `apps/cli`, sin colocar código de aplicación
directamente en la raíz del repositorio.

Todos los packages Go iniciales comparten un límite de dependencias y release.
Esto mantiene simples el desarrollo entre componentes y `go test ./...`, pero una
actualización de dependencias afecta al módulo compartido y los packages Go no
pueden versionarse de forma independiente hasta que sean extraídos a otro módulo.

La estructura depende de disciplina durante las revisiones: packages compartidos
genéricos, imports directos de implementaciones de servicios y directorios vacíos
prematuros debilitarían los límites. Las herramientas de grafos de build se
mantendrán deliberadamente limitadas hasta que el repositorio contenga suficientes
componentes para justificarlas.

La ubicación de MCP permanece como una decisión futura basada en el primer caso de
uso concreto. Esto evita tratar el nombre de un protocolo como una capa
arquitectónica antes de conocer sus responsabilidades de runtime y ownership.

## Alternatives Considered

Mantener directorios globales `api/`, `cmd/` e `internal/`. Esta alternativa sigue
un layout común de repositorios Go, pero no fue seleccionada porque hace menos
explícitos los límites entre componentes heterogéneos y aplicaciones orientadas a
actores en este monorepo.

Crear inmediatamente un módulo Go para cada aplicación, servicio y package. Esta
alternativa no fue seleccionada porque los componentes comparten inicialmente el
lifecycle de la release `v0.0.1`, y múltiples módulos agregarían sincronización de
versiones y pruebas aisladas antes de que existan releases independientes.

Restringir `apps/` a interfaces gráficas humanas. Esta alternativa no fue
seleccionada porque CLI, TUI y entry points orientados a agentes también son
aplicaciones usadas directamente por actores de la plataforma.

Clasificar ahora todo MCP server como una aplicación o un servicio. Esta
alternativa no fue seleccionada porque MCP describe una superficie de protocolo,
lo que no es información suficiente para determinar ownership de runtime o
lifecycle.

Adoptar Turborepo, Bazel u otro grafo de build desde el comienzo. Esta alternativa
no fue seleccionada porque el repositorio inicial no tiene un grafo de tareas ni
una escala de build que justifique configuración y mantenimiento adicionales.

## References

- [Go multi-module workspaces](https://go.dev/doc/tutorial/workspaces)
- [Go module repository organization](https://go.dev/doc/modules/managing-source)
- [Turborepo package types](https://turborepo.dev/docs/core-concepts/package-types)
- [Turborepo package and task graphs](https://turborepo.dev/docs/core-concepts/package-and-task-graph)
