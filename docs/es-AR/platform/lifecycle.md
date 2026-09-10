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
