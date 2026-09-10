# Contribuciones

## Requisitos previos

- Go 1.26.6, la versión utilizada por CI.
- `just` 1.57 o superior.
- Docker para las pruebas de integración con PostgreSQL.
- `kubectl` solamente para pruebas de aceptación en un clúster real.

La primera ejecución descarga los módulos Go y los binarios de Kubernetes
`envtest`. Usá `just --list` para descubrir los comandos mantenidos.

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

## Pruebas de aceptación en K3s

Estas pruebas para mantenedores no forman parte de `just verify` porque
requieren un clúster existente:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --file ./tls-setup.yaml
```

Los modos `teardown` y `cycle` del script del control plane modifican el clúster
y requieren el argumento explícito `--confirm <context>`. La verificación TLS es
de solo lectura. Ejecutá los scripts sin argumentos para ver su uso completo.
