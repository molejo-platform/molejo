# Molejo

[Inicio del proyecto](../../README.md) | [English](../en/README.md) |
[Português (Brasil)](../pt-BR/README.md)

> Proyecto experimental en alfa. Molejo todavía no está lista para
> producción.

Molejo es una Kubernetes Application Platform pública y portable. Su
objetivo es permitir que las personas creen, publiquen y operen aplicaciones sin
necesidad de conocer Kubernetes, `kubectl`, YAML ni la infraestructura subyacente.

Kubernetes es el sustrato de ejecución, no la API del producto. Los usuarios
declaran la intención del producto mediante contratos de Molejo, y controllers
confiables reconcilian esa intención en recursos de Kubernetes.

## Estado

El repositorio continúa siendo experimental y en alfa. Su implementación actual
incluye contratos de Kubernetes versionados, el Platform Operator, un backend de
control plane y el flujo de pairing del Cluster Agent outbound. Estos componentes
no constituyen una plataforma soportada para producción. Las releases alfa pueden
cambiar contratos sin compromiso de compatibilidad ni migración.

## Modelo del Producto

La jerarquía canónica es:

```text
Workspace
└── Project
    ├── App
    └── Environment
         ↘ AppEnvironment ↙
```

Un `App` es la identidad lógica de la aplicación. Un `AppEnvironment` vincula un
App con un Environment. Un `Deployment` inmutable selecciona una Release y una
revisión de configuración para ese vínculo; `AppDeployment` es su proyección en
el runtime de Kubernetes.

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

El inglés es el idioma canónico de la documentación. Las guías de arquitectura y
operación en portugués (`pt-BR`) y español de Argentina (`es-AR`) se mantienen en
conjunto. Las ADR tienen una única copia canónica en inglés para evitar
divergencias entre decisiones.

Guías actuales de arquitectura y componentes:

- [Modelo operativo](architecture/operational-model.md)
- [Inspección de la foundation](foundation/inspect.md)
- [Ciclo de vida de la plataforma](platform/lifecycle.md)
- [Platform Operator](platform/platform-operator.md)
- [Cluster Agent outbound](platform/cluster-agent.md)
- [Capacidades del clúster](capabilities/README.md)
- [Application loop](application-loop/README.md)
- [Releases con CI externa](application-loop/external-ci.md)
- [Threat model de seguridad](architecture/threat-model-de-seguridad.md)
- [Decisiones arquitectónicas canónicas](adr/README.md)

## Contribuciones

Las pautas de contribución se publicarán en
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidad

- [Código de Conducta](CODE_OF_CONDUCT.md)
- [Política de Seguridad](SECURITY.md)
- [Soporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
