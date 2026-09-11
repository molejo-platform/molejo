# Ciclo de vida de la plataforma

El platform lifecycle administra solamente componentes y contratos de Molejo:

```sh
molejoctl platform runtime install --kube-context <contexto> --version <versión>
molejoctl platform control-plane install --kube-context <contexto> --version <versión>
molejoctl platform doctor --kube-context <contexto>
molejoctl platform status --kube-context <contexto>
```

El runtime está compuesto por Platform Operator y Cluster Agent outbound. El
control plane contiene API, Console, configuración de PostgreSQL y material de
pairing instalados por el chart alfa actual. `doctor` y `status` validan el contrato
del runtime.

Las releases alfa no prometen upgrades in-place. Si la release instalada o su
configuración pública inmutable es diferente, respaldá lo que deba conservarse,
seguí el teardown experimental y reinstalá el alfa solicitado.

## Charts locales para desarrollo

Los dos comandos `install` aceptan `--chart-path` para ejecutar el mismo flujo con
un directorio o archivo `.tgz` local ya preparado. El chart debe usar la API `v2`,
tener el nombre esperado (`molejo-cluster` o `molejo-control-plane`) y contener la
misma versión indicada con `--version`.

Los directorios dentro de `deploy/charts` son insumos de la herramienta de release
y no se pueden instalar directamente: el artefacto preparado también contiene los
manifests, CRDs y digests de imágenes generados durante el empaquetado. Los charts
locales se tratan como inmutables; para probar otro contenido con la misma versión
alfa, eliminá la instalación experimental y volvé a instalarla.

## Administración de publicación HTTP

La instalación del Control Plane imprime el Cluster ID no secreto y el UID actual
de Kubernetes. Guardá el Cluster ID en un setup versionado y mantené explícito el
contexto Kubernetes para que molejoctl bloquee un clúster recreado o incorrecto
antes de modificar el producto.

```yaml
apiVersion: config.molejo.dev/v1alpha1
kind: HTTPPublicationSetup
metadata:
  name: molejo-dev
spec:
  clusterId: cls-abcdefghijklmnopqrst
  binding:
    schemaVersion: kubernetes-http.v1alpha1
    gatewayNamespace: molejo-system
    gatewayName: molejo
    listeners:
      - name: https-apex
        hostname: molejo.dev
      - name: https-apps
        hostname: "*.molejo.dev"
  domains:
    - id: home
      kind: Exact
      name: molejo.dev
      reservedNames: []
      workspaceIds:
        - ws-abcdefghijklmnopqrst
    - id: apps
      kind: SubdomainPool
      name: molejo.dev
      reservedNames:
        - admin.molejo.dev
      workspaceIds:
        - ws-abcdefghijklmnopqrst
```

Ejecutá el plan de solo lectura, revisá las operaciones y después aplicá y verificá:

```sh
molejoctl capability publication plan --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml
molejoctl capability publication apply --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml --yes
molejoctl capability publication verify --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml
```

Usá `--ca-file` con una CA privada del Control Plane. Las credenciales se solicitan
solamente en una terminal interactiva. `status` y `dependents` leen el mismo estado
de API; las listas devuelven una página limitada y un cursor explícito. La eliminación
nunca se infiere del archivo. Usá `grant revoke`, `domain delete` y
`binding disconnect` después de quitar referencias Desired, Applied y Executable.
No existe una opción force.

El Binding declara listeners existentes del Gateway. No instala un Gateway, emite
o renueva certificados, edita DNS ni demuestra alcance público. Esas responsabilidades
siguen con los owners de infraestructura configurados; los nombres de DNS privado
son válidos cuando el routing y los grants declarados son válidos.
