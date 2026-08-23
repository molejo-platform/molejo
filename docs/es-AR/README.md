# Fruto Platform

[Inicio del proyecto](../../README.md) | [English](../en/README.md) |
[Português (Brasil)](../pt-BR/README.md)

> Proyecto experimental en pre-alfa. Fruto Platform todavía no está lista para
> producción.

Fruto Platform es una Kubernetes Application Platform pública y portable. Su
objetivo es permitir que las personas creen, publiquen y operen aplicaciones sin
necesidad de conocer Kubernetes, `kubectl`, YAML ni la infraestructura subyacente.

Kubernetes es el sustrato de ejecución, no la API del producto. Los usuarios
declaran la intención del producto mediante contratos de Fruto, y controllers
confiables reconcilian esa intención en recursos de Kubernetes.

## Estado

El repositorio está en su etapa fundacional. El trabajo es intencionalmente
incremental: se implementa la capacidad útil más pequeña, se la observa en
ejecución, se la corrige a partir de evidencia real y solo entonces se la amplía.

El repositorio ahora contiene un corte vertical ejecutable: un contrato
`AppDeployment`, un operator de Kubernetes, un Service ClusterIP privado,
publicación opcional mediante HTTPRoute y un Gateway HTTPS compartido, y pruebas
reproducibles de integración y end-to-end. También contiene contratos de imagen
para HTML estático y SPA Vite/React que reutilizan la misma API de runtime. Todavía no existe una API pública del
producto, CLI, interfaz web, automatización de DNS externo ni gestión de workloads
lista para producción.

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

Este repositorio es el monorepo público de Fruto Platform. Contendrá los contratos
versionados y los componentes que implementan el producto público.

La estructura se incorporará solo cuando cada componente tenga un consumidor real:

- `apps/` — aplicaciones usadas directamente por personas o agentes de software;
- `services/` — componentes server-side ejecutables de manera independiente;
- `packages/` — bibliotecas reutilizables, contratos de Kubernetes y SDKs generados;
- `contracts/` — definiciones canónicas de interfaces independientes del lenguaje;
- `deploy/` — artefactos de instalación de Kubernetes generados y mantenidos;
- `test/e2e/` — pruebas que atraviesan límites de componentes;
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

## Desarrollo

El checkout actual requiere Go 1.26 o posterior, Node.js 24.19.0 con Corepack,
Docker con Buildx, `kubectl` y `just`. El gate local exige intencionalmente la
versión exacta de Node fijada en `.node-version`. No es necesario instalar Kind
globalmente; el comando end-to-end ejecuta la versión fijada mediante Go.

```bash
just generate  # regenera artefactos DeepCopy, CRD y RBAC
just test      # ejecuta pruebas contra un API server local de envtest
just verify    # genera, verifica formato, ejecuta go vet y las pruebas
just e2e       # valida rutas privadas y públicas en un cluster Kind descartable
just ci        # ejecuta el gate local determinístico completo
just e2e-public # valida por separado acceso HTTPS público de salida
just frontend-check # verifica tipos de la fixture React y de la Console desde el lockfile pnpm
just frontend-test # ejecuta las pruebas de la Console y valida ambas imágenes en un container restringido
just audit-frontend-images # ejecuta la auditoría opcional con Docker Scout
```

La primera ejecución descarga módulos Go, binarios de envtest, Kind e imágenes de
container fijados. `just e2e` usa un kubeconfig temporal y no accede al contexto
de Kubernetes seleccionado actualmente. Construye dos versiones locales de la
fixture HTTP, referencia ambas por digest y valida HTTP privado y publicación
HTTPS de REST, GraphQL, SSE y WebSocket mediante un Gateway local con un
certificado efímero confiado por el cliente de prueba. También valida hosting
estático, deep links de la SPA, cache, probes, rollout, drift, self-healing y
garbage collection. `just e2e-public` agrega solo
una llamada HTTPS real de salida y queda fuera del gate determinístico `just ci`.
El DNS público y un certificado con confianza pública requieren una aceptación
separada en el ambiente de foundation.

`just audit-frontend-images` queda deliberadamente fuera de `just ci`. Requiere
Docker Scout y usa su base mutable de vulnerabilidades para verificar hallazgos
críticos y altos del sistema operativo y npm en el builder descartado de la SPA y
realiza un análisis crítico/alto completo de ambas imágenes de runtime.

El target manual `just e2e-frontend-k3s` está reservado para mantenedores con
acceso a `fruto-lab`. Despliega imágenes del registry privado por digest y deja
`static.fruto.calouro.tech` y `spa.fruto.calouro.tech` disponibles para inspección.

## Operación

El [runbook del platform operator](operations/platform-operator.md) documenta el
contrato de estado, flujo de diagnóstico, métricas protegidas y tracing opcional.

## Documentación

El inglés es el idioma canónico de la documentación. Las versiones en portugués
(`pt-BR`) y español de Argentina (`es-AR`) se mantienen en conjunto, y se podrán
incorporar otros idiomas en el futuro.

Los Architecture Decision Records se encuentran en
[`adr`](adr/README.md). Los archivos de ADR usan el mismo identificador,
nombre de archivo, título en inglés y headings en inglés en todos los idiomas;
solo se localiza el texto debajo de esos headings.

## Contribuciones

Las pautas de contribución se publicarán en
[CONTRIBUTING.md](CONTRIBUTING.md).

## Comunidad

- [Código de Conducta](CODE_OF_CONDUCT.md)
- [Política de Seguridad](SECURITY.md)
- [Soporte](SUPPORT.md)
- [Mantenedores](MAINTAINERS.md)
- [Apache License 2.0](../../LICENSE)
