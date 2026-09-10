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
- [ADR de identidad y pairing del Cluster Agent outbound](adr/0013-outbound-cluster-agent-identity-and-pairing.md)
- [ADR del límite de Release y deploy con CI externa](adr/0014-limite-de-release-y-deploy-con-ci-externa.md)
- [ADR de ownership de capacidades](adr/0015-ownership-de-capacidades.md)
- [ADR de política de ciclo de vida alfa](adr/0016-politica-de-ciclo-de-vida-alfa.md)
- [ADR del límite de identidad humana](adr/0017-limite-de-identidad-humana.md)
- [ADR de observación de capacidades y disponibilidad](adr/0018-observacion-de-capacidades-y-disponibilidad-de-features.md)
- [ADR de provisionamiento de Workspace y namespace](adr/0019-provisionamiento-de-workspace-y-limite-de-namespace.md)
- [ADR de custodia de secrets y entrega](adr/0020-custodia-de-secrets-y-entrega-al-runtime.md)
- [ADR de bindings explícitos gestionados por el operador](adr/0021-bindings-explicitos-gestionados-por-el-operador.md)
- [ADR de métricas neutrales con consulta Prometheus-compatible](adr/0022-metricas-neutrales-con-consulta-prometheus.md)
- [ADR del límite de la stack frontend de la Consola](adr/0023-limite-de-la-stack-frontend-de-la-consola.md)

## Contribuciones

Las pautas de contribución se publicarán en
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidad

- [Código de Conducta](CODE_OF_CONDUCT.md)
- [Política de Seguridad](SECURITY.md)
- [Soporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
