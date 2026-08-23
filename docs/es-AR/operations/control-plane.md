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

## Recuperación y fronteras

Las migrations son forward-only y están protegidas por serialización transaccional
de PostgreSQL. Un Job de migration fallido puede ejecutarse de nuevo después de
revisar sus logs. Un runtime ausente o indisponible se muestra como `Unknown`,
nunca como deployment exitoso. Esta instalación no declara HA ni DR.
