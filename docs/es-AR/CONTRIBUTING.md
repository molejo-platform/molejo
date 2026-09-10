# Contribuciones

## Requisitos previos

- Go 1.26.6, la versión utilizada por CI.
- Node.js 24 o superior con Corepack habilitado.
- `just` 1.57 o superior.
- Docker para las pruebas de integración con PostgreSQL.
- `kubectl` solamente para pruebas de aceptación en un clúster real.

La primera ejecución descarga los módulos Go y los binarios de Kubernetes
`envtest`. Instalá las dependencias de Console con
`corepack pnpm install --frozen-lockfile`. Usá `just --list` para descubrir los
comandos mantenidos.

## Código generado

Editá las fuentes autoritativas y ejecutá `just generate`; no edites directamente
los archivos generados:

- `contracts/molejo/clusteragent/v1alpha1/agent.proto` genera el contrato Go y
  gRPC del Agent.
- `contracts/openapi/control-plane-v1.yaml` genera los tipos del servidor Go del
  Control Plane y los tipos TypeScript de Console.
- `packages/kubernetes-api/apis/` genera el código de deep copy y `deploy/crds/`.
- Las migraciones y consultas del Control Plane generan
  `services/control-plane-api/internal/store/sqlc/`.

Inspeccioná siempre el diff generado antes de realizar el commit.

## Pruebas

Ejecutá la suite rápida sin servicios externos:

```bash
just test
```

Las pruebas de integración con PostgreSQL requieren Docker en ejecución.
Testcontainers inicia PostgreSQL 17.6 en un puerto aleatorio del host y lo
elimina después de la suite de cada paquete:

```bash
just integration-test
```

Para usar una instancia PostgreSQL existente en lugar de Docker, proporcioná
su URL:

```bash
MOLEJO_TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' just integration-test
```

`just verify` ejecuta ambas suites y todos los controles de calidad del
repositorio.
`just ci` también verifica que la generación y el formato no modifiquen el
worktree.

Durante el desarrollo, usá primero el comando del menor alcance relevante:

```bash
just operator-test
just cluster-agent-test
just control-plane-test
just contract-test
just distribution-test
```

Console también puede verificarse de forma independiente:

```bash
just frontend-check
just frontend-test
just frontend-build
```

## Estrategia de pruebas

- Platform Operator: pruebas puras de renderizado, después `envtest`, usando Kind
  solamente cuando el comportamiento requiera un clúster real.
- Cluster Agent: decisiones basadas en tablas, TLS real con gRPC en memoria y
  clientes Kubernetes simulados solamente para efectos exactos de persistencia.
- Control Plane: pruebas puras de dominio e integraciones controladas; usá
  PostgreSQL real para transacciones, restricciones, migraciones, concurrencia e
  idempotencia.
- Console: controles estáticos y pruebas de integración con Vitest y Testing
  Library; usá Playwright solamente cuando el comportamiento dependa del navegador
  o de la integración completa entre frontend y backend.

Preferí resultados observables y aserciones que esperen el estado final antes que
detalles de implementación y esperas de duración fija. Agregá pruebas end-to-end
amplias solamente cuando el comportamiento no pueda probarse en una capa inferior.
Nunca incluyas credenciales, claves privadas, certificados o kubeconfigs en
snapshots.

## Pruebas de aceptación en K3s

Estas pruebas para mantenedores no forman parte de `just verify` porque
requieren un clúster existente:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --file ./tls-setup.yaml
```

Los modos `teardown` y `cycle` del script del Control Plane modifican el clúster
y requieren el argumento explícito `--confirm <context>`. La verificación TLS es
de solo lectura. Ejecutá los scripts sin argumentos para ver su uso completo.

## Commits

- Usá mensajes Conventional Commits en inglés.
- Agregá un cuerpo al commit cuando el cambio incluya más de tres archivos.
- Agregá al stage solamente los archivos que pertenezcan al cambio.
