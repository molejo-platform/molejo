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
