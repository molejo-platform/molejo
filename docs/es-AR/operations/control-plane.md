# Operaciones del Control Plane

El control plane es pre-alpha y está destinado a un Workspace beta local.

## Bootstrap local

Iniciá PostgreSQL con `just db-up` y ejecutá `just db-migrate`. Generá hashes
Argon2id enviando la contraseña por stdin a `go run
./services/control-plane-api/cmd/control-plane-api hash-password`. Configurá
`FRUTO_OWNER_PASSWORD_HASH`, `FRUTO_TESTER_1_PASSWORD_HASH` y
`FRUTO_TESTER_2_PASSWORD_HASH` fuera de Git y ejecutá el comando `bootstrap`.

La API usa `KUBECONFIG` cuando corre en el host. En el cluster, configurá
`FRUTO_IN_CLUSTER=true` y proveé el Secret de base de datos fuera del
repositorio.

## Evidencia local de la Fase 6

`just control-plane-e2e-kind` crea un cluster Kind descartable y comprueba las
dos topologías. La primera ejecuta el binario de la API con `KUBECONFIG`
temporario y Vite en el host, mientras PostgreSQL corre en Docker. La segunda
ejecuta API, Console, Job de migration, Services y HTTPRoutes dentro de Kind.

La suite pinada de Playwright valida login, el ciclo completo de deployment,
historial, deep links, sesión expirada y credenciales inválidas. El runner
también comprueba la recuperación de una operación pendiente después de
reiniciar la API, `Unknown` mientras se pausa el API server de Kind, idempotencia
por repetición y concurrencia y la ServiceAccount del control plane con
`kubectl auth can-i`. Los Secrets se generan durante la ejecución y los
diagnósticos se recopilan antes de eliminar los recursos temporarios.

## Recuperación y fronteras

Las migrations son forward-only y están protegidas por serialización transaccional
de PostgreSQL. Un Job de migration fallido puede ejecutarse de nuevo después de
revisar sus logs. Un runtime ausente o indisponible se muestra como `Unknown`,
nunca como deployment exitoso. Esta instalación no declara HA ni DR.

NetworkPolicy queda fuera del gate local principal hasta seleccionar y probar
explícitamente un CNI compatible.
