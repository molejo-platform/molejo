# Operaciones del Control Plane

El control plane es pre-alpha y está destinado a un Workspace beta local.

## Bootstrap local

Iniciá PostgreSQL con `just db-up` y ejecutá `just db-migrate`. Generá hashes
Argon2id enviando la contraseña por stdin a `go run
./services/control-plane-api/cmd/control-plane-api hash-password`. Configurá
`FRUTO_OWNER_PASSWORD_HASH`, `FRUTO_TESTER_1_PASSWORD_HASH` y
`FRUTO_TESTER_2_PASSWORD_HASH` fuera de Git y ejecutá el comando `bootstrap`.

La API usa `KUBECONFIG` cuando corre en el host y también exige
`FRUTO_EXPECTED_KUBE_CONTEXT`, `FRUTO_EXPECTED_KUBE_SERVER` y
`FRUTO_EXPECTED_CLUSTER_UID`. Rechaza mutaciones cuando cualquier identidad
difiere. En el cluster, configurá `FRUTO_IN_CLUSTER=true`, proveé el UID esperado
del cluster y el Secret de base de datos fuera del repositorio.

HTTP sin TLS se acepta solamente en el perfil explícito de desarrollo sobre
loopback o `*.localhost`. Para TLS local confiable por el navegador, ejecutá
`just dev-tls-cert` una vez y después `just dev-api-tls` y
`just dev-frontend-tls` en terminales separadas. El certificado y la clave quedan
en `.local/certs`, ignorado por Git, y `mkcert` instala la CA local en el trust
store. Los equivalentes HTTP son `just dev-api-http` y
`just dev-frontend-http`.

El origen público de la Consola es `https://cloud.molejo.dev`. La Fase 7 reserva
`https://cloud.molejo.dev/api/v1/github/callback` como callback exacta del OAuth
de GitHub. La API deberá atender la callback a través de la ruta `/api`; el client secret
debe permanecer solamente en el servidor y fuera de Git.

## Evidencia local de la Fase 6

`just control-plane-e2e-kind` crea un cluster Kind descartable y comprueba las
dos topologías. La primera ejecuta el binario de la API con `KUBECONFIG`
temporario y Vite en el host, mientras PostgreSQL corre en Docker. La segunda
ejecuta API, Console, Job de migration, Services y HTTPRoutes dentro de Kind.

Las decisiones puras y los clientes HTTP del frontend se ejecutan primero como
tests unitarios Node, sin React ni DOM. Los tests de integración React/jsdom
cubren login, sesión vencida, formulario, rutas y estados del producto.
Playwright queda limitado a un test estratégico: login y ciclo completo de
create, observe, update, historial y delete. La suite Go con PostgreSQL comprueba
por separado el rechazo de sesiones expiradas y revocadas. El runner
también comprueba la recuperación de una operación pendiente después de
reiniciar la API, `Unknown` mientras se pausa el API server de Kind, idempotencia
por repetición y concurrencia y la ServiceAccount del control plane con
`kubectl auth can-i`. Los Secrets se generan durante la ejecución y los
diagnósticos se recopilan antes de eliminar los recursos temporarios.

## Recuperación y fronteras

Las migrations son forward-only, aplicadas por Goose pinado y protegidas por un
advisory lock de sesión de PostgreSQL. Los tipos generados por SQLC permanecen
dentro del adapter PostgreSQL. Un Job de migration fallido puede ejecutarse de nuevo después de
revisar sus logs. Un runtime ausente o indisponible se muestra como `Unknown`,
nunca como deployment exitoso. Esta instalación no declara HA ni DR.

Antes de la aceptación en k3s, `just ci` debe pasar desde un checkout limpio.
Registrá el commit de origen, `linux/amd64` y los digests de API, Consola y
Testkit en un manifiesto externo de release; reemplazá los placeholders de UID
del cluster y proxy confiable; creá Secrets de base y bootstrap fuera de Git; y
declará si la base es descartable. Si no lo es, generá y verificá una exportación
manual cifrada antes del rollout. Rollback significa reaplicar digests
compatibles; una migration forward desconocida por el binario anterior bloquea
el rollback. La recuperación es restauración manual en otro PostgreSQL, seguida
por `SchemaReady` y prueba funcional. Esto no es disaster recovery de producción.

Ejecutá el preflight read-only `just control-plane-preflight-k3s` con contexto,
servidor, UID del cluster y digests aprobados. Después del éxito, usá
`just control-plane-render-release` para renderizar Kustomize pinado por digest
en una ruta absoluta fuera del checkout. Ninguno de los comandos aplica
recursos; la mutación del cluster sigue siendo un paso manual con autorización
separada.

## Aceptación k3s de la Fase 7

Usá un checkout limpio y con commit y un directorio externo con modo `0700`. El
overlay de laboratorio fija PostgreSQL 17.6 por el digest del manifiesto
`linux/amd64`, lo agenda en `fruto-data-01`, solicita un volumen `local-path` de
2 GiB y marca servicio y storage como fixtures pre-alpha descartables. Esto no
es HA ni una base administrada.

```bash
export FRUTO_RELEASE_DIR=/private/tmp/molejo-control-plane-release
export FRUTO_K3S_CONTEXT=fruto-lab
export FRUTO_EXPECTED_KUBE_SERVER='<approved-kube-api-url>'
export FRUTO_EXPECTED_CLUSTER_UID='<approved-kube-system-uid>'
export FRUTO_TRUSTED_PROXY_CIDR='<approved-pod-cidr>'
export FRUTO_TESTKIT_IMAGE=ghcr.io/molejo-platform/testkit@sha256:1b5a36a776cc16dd3fa728c2269109ca45fca2a4af622b3a166e4e578b9cdb08

just ci
just control-plane-build-release
source "$FRUTO_RELEASE_DIR/images.env"
just control-plane-preflight-k3s
export FRUTO_RELEASE_OUTPUT="$FRUTO_RELEASE_DIR/control-plane.yaml"
just control-plane-render-release
just control-plane-prepare-k3s
just control-plane-apply-k3s
just control-plane-accept-k3s
```

La preparación genera la contraseña del owner, su hash Argon2id y las
credenciales de PostgreSQL bajo `$FRUTO_RELEASE_DIR/secrets`, con modo `0600`;
solo referencias a Secrets llegan a los PodSpecs. También copia la credencial de
pull del registry a `fruto-control-plane` sin escribirla en el repositorio ni en
la terminal. Recuperá la contraseña del owner localmente para la aceptación en
el navegador y rotala después de la prueba. No pegues contraseñas, hashes, URLs
de base, datos de Secret ni kubeconfigs en issues, logs, commits o chat.

Después de la aceptación automatizada, usá la Consola para crear como `Public`
el digest registrado de Testkit, esperá `Ready`, actualizalo, inspeccioná el
historial, reiniciá `deployment/control-plane-api`, recargá la misma sesión del
navegador y eliminá el deployment. Registrá solamente IDs públicos, digests,
estados de operación, condiciones de rutas y conteos. Una segunda aplicación del
mismo bundle es la prueba de rollback de la primera release; otro digest anterior
solo puede reaplicarse si su binario comprende todas las migrations forward ya
aplicadas.

Como esta base de laboratorio es explícitamente descartable, recuperación
significa recrear la fixture, ejecutar migrations y bootstrap y comprobar
`SchemaReady` más el flujo funcional. Si la instalación deja de ser descartable,
detenete y probá una exportación manual cifrada y una restauración en otro
PostgreSQL antes del rollout. Ninguna opción declara disaster recovery de
producción.

Los máximos de réplicas, CPU y memoria son cuotas de producto aplicadas por la
API pública. El CRD aplica intencionalmente solo la validez de runtime y las
relaciones entre requests y limits; no duplica esas cuotas de producto.

La NetworkPolicy del repositorio es un ejemplo incompleto y no instalado. El
egress hacia PostgreSQL no restringe destino porque la instalación portátil aún
no tiene un contrato de destino de la base. No la instales tal como está:
primero definí el destino de la base y el CNI y después validá la política.
