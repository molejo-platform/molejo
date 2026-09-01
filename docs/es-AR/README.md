# Molejo

[Inicio del proyecto](../../README.md) | [English](../en/README.md) |
[Português (Brasil)](../pt-BR/README.md)

> Proyecto experimental en pre-alfa. Molejo todavía no está lista para
> producción.

Molejo es una Kubernetes Application Platform pública y portable. Su
objetivo es permitir que las personas creen, publiquen y operen aplicaciones sin
necesidad de conocer Kubernetes, `kubectl`, YAML ni la infraestructura subyacente.

Kubernetes es el sustrato de ejecución, no la API del producto. Los usuarios
declaran la intención del producto mediante contratos de Molejo, y controllers
confiables reconcilian esa intención en recursos de Kubernetes.

## Estado

El repositorio está en su etapa fundacional. El trabajo es intencionalmente
incremental: se implementa la capacidad útil más pequeña, se la observa en
ejecución, se la corrige a partir de evidencia real y solo entonces se la amplía.

El alcance actual está limitado a convenciones de ingeniería, decisiones de
arquitectura y los primeros contratos versionados. Todavía no existe una
plataforma funcional, API pública, controller ni interfaz web.

## Modelo del Producto

La jerarquía canónica es:

```text
Workspace → Project → Environment → App
```

Un `App` es la identidad lógica de la aplicación. Un `AppDeployment` representa el
despliegue de una release específica de un App en un Environment.

La plataforma continúa siendo la fuente confiable de identidad, ownership y
autorización del producto. Los nombres, namespaces, labels y annotations de
Kubernetes son proyecciones de runtime y nunca otorgan permisos del producto.

## Monorepo

Este repositorio es el monorepo público de Molejo. Contendrá los contratos
versionados y los componentes que implementan el producto público.

La estructura se incorporará solo cuando cada componente tenga un consumidor real:

- `api/` — tipos de la API de Kubernetes y contratos versionados;
- `cmd/` — entry points en Go para controllers, APIs y otros binarios;
- `internal/` — implementación Go compartida y privada;
- `web/` — interfaz web del producto;
- `config/` — artefactos de instalación de Kubernetes generados y mantenidos;
- `docs/` — documentación pública de arquitectura y del proyecto.

El código Go inicial usará un único módulo en la raíz del repositorio. Se
incorporarán nuevos módulos Go y un archivo `go.work` solamente cuando un
componente, como un SDK público, requiera versionado y compatibilidad de releases
independientes.

## Dirección Tecnológica

- Go, Kubebuilder y `controller-runtime` para controllers de Kubernetes;
- Go, `net/http` y Chi para APIs HTTP;
- TypeScript, React, Vite, Tailwind CSS, shadcn/ui y Lineicons para la interfaz web;
- Buildx y BuildKit para builds de containers;
- un `justfile` en la raíz para comandos de desarrollo local.

Estas elecciones describen la dirección inicial. Los componentes se incorporan
de forma incremental y no reciben scaffold antes del inicio de su fase.

## Documentación

El inglés es el idioma canónico de la documentación. Las versiones en portugués
(`pt-BR`) y español de Argentina (`es-AR`) se mantienen en conjunto, y se podrán
incorporar otros idiomas en el futuro.

Los Architecture Decision Records se encuentran en
[`adr`](adr/README.md). Los archivos de ADR usan el mismo identificador,
nombre de archivo y headings en inglés en todos los idiomas; solo se localiza el
contenido.

## Contribuciones

Las pautas de contribución se publicarán en
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidad

- [Código de Conducta](CODE_OF_CONDUCT.md)
- [Política de Seguridad](SECURITY.md)
- [Soporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
