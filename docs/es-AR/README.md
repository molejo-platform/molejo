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

El repositorio continúa siendo experimental y pre-alfa. Su implementación actual
incluye contratos de Kubernetes versionados, el Platform Operator, un backend de
control plane y el flujo de pairing del Cluster Agent outbound. Estos componentes
no constituyen una plataforma soportada para producción ni evidencia de una
release pública soportada.

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

Este repositorio es el monorepo público de Molejo. Su estructura actual es:

- `contracts/` — contratos versionados neutrales de lenguaje y generados;
- `packages/` — bibliotecas compartidas con consumidores concretos;
- `services/` — Platform Operator, control plane y Cluster Agent;
- `deploy/` — artefactos de instalación de Kubernetes generados y mantenidos;
- `docs/` — documentación pública de arquitectura y operaciones.

El código Go usa un único módulo en la raíz del repositorio. Se
incorporarán nuevos módulos Go y un archivo `go.work` solamente cuando un
componente, como un SDK público, requiera versionado y compatibilidad de releases
independientes.

## Dirección Tecnológica

- Go, Kubebuilder y `controller-runtime` para controllers de Kubernetes;
- Go, `net/http` y Chi para APIs HTTP;
- Protocol Buffers y gRPC para el canal autenticado del Cluster Agent;
- Buildx y BuildKit para builds de containers;
- un `justfile` en la raíz para comandos de desarrollo local.

Estas elecciones describen la dirección inicial. Los componentes se incorporan
de forma incremental y no reciben scaffold antes del inicio de su fase.

## Documentación

El inglés es el idioma canónico de la documentación. Las versiones en portugués
(`pt-BR`) y español de Argentina (`es-AR`) se mantienen en conjunto, y se podrán
incorporar otros idiomas en el futuro.

Guías actuales de los componentes:

- [Platform Operator](operations/platform-operator.md)
- [Cluster Agent outbound](operations/cluster-agent.md)
- [TLS del clúster](operations/tls.md)
- [Setup day zero en K3s](operations/cluster-setup.md)
- [Acceso al registry de aplicaciones](operations/registry-access.md)
- [ADR de identidad y pairing del Cluster Agent outbound](adr/0013-outbound-cluster-agent-identity-and-pairing.md)

## Contribuciones

Las pautas de contribución se publicarán en
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidad

- [Código de Conducta](CODE_OF_CONDUCT.md)
- [Política de Seguridad](SECURITY.md)
- [Soporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
