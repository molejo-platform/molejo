# Operaciones del Control Plane

El control plane es pre-alpha. Un Actor puede seleccionar entre los Workspaces de
los que es miembro; `owner` administra la jerarquía y `tester` es de solo lectura.

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

La Consola crea Workspaces, Projects, Environments y Apps exclusivamente mediante
la API REST autenticada. Un AppEnvironment exige App y Environment del mismo
Project y controla branch y configuración de runtime. Cada Deployment es un
snapshot inmutable de Release y configuración. Los cambios de runtime crean
revisiones inmutables; los cambios solamente de branch no. Un Deployment exige
Release y revisión exactas, la versión actual del AppEnvironment y el Deployment
actual revisado. El preview informa el impacto sin valores secretos. La API devuelve `404` para recursos fuera de la
membership del Actor y `403` cuando un tester intenta una mutación.

La migration 011 reemplaza el modelo experimental de deployment mutable por
AppEnvironments y Deployments inmutables. Limpia intencionalmente el historial
existente de Builds, Releases, Deployments y operaciones, preservando Actors,
Workspaces, Projects, Apps, Environments y conexiones GitHub. El Job aplica las
migrations forward en orden y nunca imprime credenciales de conexión.

El origen público de la Consola es `https://cloud.molejo.dev`. El acceso a los
repositorios usa una GitHub App, siguiendo un modelo de instalación en lugar de
una OAuth App clásica. Configurá como Setup URL exacta
`https://cloud.molejo.dev/api/v1/github/installations/callback` y como callback
exacta de autorización del usuario `https://cloud.molejo.dev/api/v1/github/callback`,
con wildcard deshabilitado. Habilitá solamente `Contents` de repositorio en modo
lectura; `Metadata` permanece en lectura de forma predeterminada. No habilités
webhooks, checks, escritura, Device Flow ni autorización OAuth durante la
instalación.

La API persiste la instalación del Workspace y el ID inmutable del repositorio,
pero no persiste tokens de usuario o de instalación de GitHub. El token temporal
del usuario se revoca después de comprobar el ownership; los tokens de instalación
se emiten bajo demanda y se descartan después de cada request.
Solamente un owner puede conectar, desconectar o cambiar la fuente de una App;
los miembros pueden consultar la fuente seleccionada. Cada App tiene como máximo
un repositorio, mientras varias Apps pueden usar el mismo repositorio. La
desconexión se rechaza mientras alguna App todavía referencie la instalación.

Proporcioná App ID, Client ID, slug, client secret y clave privada RSA mediante
un Secret `molejo-github-app` aplicado fuera de Git. El deployment monta las dos
credenciales como archivos e inicia normalmente cuando ese Secret opcional no
existe; en ese caso, los endpoints de GitHub responden
`github_not_configured`. Usá
`deploy/control-plane/github-app-secret.example.yaml` solamente como referencia
de estructura y nunca coloques credenciales reales en Git.

## Evidencia local del control plane

`just control-plane-e2e-kind` crea un cluster Kind descartable y comprueba las
dos topologías. La primera ejecuta el binario de la API con `KUBECONFIG`
temporario y Vite en el host, mientras PostgreSQL corre en Docker. La segunda
ejecuta API, Console, Job de migration, Services y HTTPRoutes dentro de Kind.

Las decisiones puras y los clientes HTTP del frontend se ejecutan primero como
tests unitarios Node, sin React ni DOM. Los tests de integración React/jsdom
cubren login, sesión vencida, formulario, rutas y estados del producto.
Playwright queda limitado a un test estratégico: login y el flujo principal de
organización de Project, Environment y App. La suite Go con PostgreSQL comprueba
por separado el rechazo de sesiones expiradas y revocadas. El runner shell
comprueba el ciclo de deployment, la recuperación de una operación pendiente
después de una falla del worker, `Unknown` mientras se pausa el API server de
Kind, idempotencia por repetición y concurrencia y las ServiceAccounts
restringidas con `kubectl auth can-i`. Los Secrets se generan durante la
ejecución y los diagnósticos se recopilan antes de eliminar los recursos
temporarios.

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
export MOLEJO_BUILD_IMAGE_REPOSITORY='<registry>/<prefijo-de-repositorio>'

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

Después de la aceptación automatizada, usá la Consola para conectar el App
Testkit a un Environment, configurarlo como `Public`, construir su branch y
desplegar la Release resultante. Esperá `Ready`, actualizá el AppEnvironment,
inspeccioná el historial de Deployments, reiniciá `deployment/control-plane-api`,
recargá la misma sesión del navegador y eliminá el AppEnvironment. Registrá solamente IDs públicos, digests,
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

## Parámetros de runtime de la Fase 9

Los parámetros se catalogan por Workspace como valores `PlainText` o `Secret`
write-only. Un AppEnvironment vincula el nombre de una variable de entorno a una
versión inmutable del parámetro; cada Deployment captura esos vínculos y su
`configurationVersion`. El runtime worker resuelve el snapshot, guarda valores
comunes en un ConfigMap inmutable, materializa secrets de OpenBao en un Secret
inmutable de Kubernetes y proyecta en AppDeployment solamente los nombres de
esos objetos.

Después de observar la nueva generación como `Ready`, el worker elimina objetos
de configuración anteriores que tengan la annotation exacta de owner del
control plane, la label managed-by, el nombre determinístico y la label de
versión. Al eliminar un AppEnvironment se recolectan todos sus objetos de
configuración restantes. La Role del Workspace permite que solo ese worker liste
y elimine ConfigMaps y Secrets en ese Namespace; la ServiceAccount de la API
pública no puede leerlos.

La instalación OpenBao incluida es una fixture de laboratorio de un nodo con
unseal manual. Ejecutá `just openbao-prepare-k3s` con el contexto aprobado
`fruto-lab` y mantené el material de inicialización y fingerprint fuera de Git.
Esta fixture no es un servicio de secrets de alta disponibilidad ni listo para
producción.

## Build plane de la Fase 8

Un owner inicia un Build para un AppEnvironment cuyo App tiene fuente GitHub. La
API registra el commit exacto de la branch del AppEnvironment antes de encolarlo.
El worker acepta
solamente un `Dockerfile` en la raíz, construye `linux/amd64`, publica un tag con
el SHA del commit y promueve una Release solamente después de registrar el digest
OCI. Los logs son limitados y sanitizados. Un Build fallido nunca crea una
Release.

El build plane se instala por separado en `molejo-builds`. Su daemon BuildKit es
rootless y solamente el worker puede acceder mediante TLS mutuo. El modo rootless
upstream para Kubernetes requiere seccomp/AppArmor unconfined y
`--oci-worker-no-process-sandbox`; esta es una frontera pre-alpha explícita, no
una declaración de aislamiento de producción. CPU, memoria, disco temporario,
concurrencia de un build y timeout de 15 minutos limitan la primera
implementación.
Ambos Pods seleccionan el rol de node `runtime` existente en el laboratorio y
`amd64`; esto mantiene builds no confiables fuera de los nodes de control plane y
datos, aunque todavía comparte un node con workloads administrados. Se permite
egress HTTP/HTTPS público para dependencias del Dockerfile, mientras rangos
privados, link-local y del cluster permanecen denegados salvo las rutas
explícitas de DNS, PostgreSQL y BuildKit.

Desde un checkout limpio y con commit, prepará un directorio externo de release y
ejecutá:

```bash
export FRUTO_RELEASE_DIR=/private/tmp/molejo-control-plane-release
export FRUTO_EXPECTED_CLUSTER_UID='<uid-aprobado-de-kube-system>'
export FRUTO_TRUSTED_PROXY_CIDR='<cidr-aprobado-de-pods>'
export FRUTO_TESTKIT_IMAGE='<referencia-aprobada-de-testkit-por-digest>'
export MOLEJO_BUILD_IMAGE_REPOSITORY='<registry>/<prefijo-de-repositorio>'

just ci
just control-plane-build-release
just builds-build-release
source "$FRUTO_RELEASE_DIR/images.env"
source "$FRUTO_RELEASE_DIR/builds.env"
export FRUTO_RELEASE_OUTPUT="$FRUTO_RELEASE_DIR/control-plane.yaml"
export FRUTO_BUILDS_RELEASE_OUTPUT="$FRUTO_RELEASE_DIR/builds.yaml"
just control-plane-render-release
just builds-render-release

export GITHUB_APP_ID='<github-app-id>'
export GITHUB_APP_PRIVATE_KEY_FILE='<pem-protegido-de-github-app>'
just control-plane-prepare-k3s
just builds-prepare-k3s
just control-plane-apply-k3s
just builds-apply-k3s
```

El comando de preparación siempre usa el contexto Kubernetes `fruto-lab`, deriva
una URL cross-namespace del Secret existente de la base del control plane, crea
una CA privada y certificados de servidor/cliente fuera de Git, monta la clave
de GitHub App solamente en el worker y copia la credencial existente del registry
a `molejo-builds`. La URL de base debe usar el Service cross-namespace
`postgres.fruto-control-plane.svc`. `MOLEJO_BUILD_DATABASE_URL_FILE` puede
reemplazar esa fuente con un archivo protegido. Renderizado, preparación de Secrets y apply
son gates separados; no ejecutes comandos mutables sin autorización explícita
para sus recursos exactos.

La aceptación requiere crear un Build desde la Consola, observar el SHA
registrado y logs limitados, ver una Release fijada por digest solamente después
del éxito y crear un deployment desde esa Release. Comprobá que un Dockerfile
fallido no crea Release y que una actualización no reemplaza la imagen controlada
por la Release. Registrá solamente IDs públicos, SHAs, digests, estados y logs
sanitizados.
