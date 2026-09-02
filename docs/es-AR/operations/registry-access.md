# Acceso al registry de aplicaciones

`RegistrySetup` es un runbook local y versionado de `molejoctl`. Prepara un
namespace existente para descargar imágenes de aplicaciones desde un registry
privado. No es una CRD de Kubernetes, no se almacena en el control plane y no
configura nodes, IAM, container runtimes, DNS ni rutas de red.

Generá e inspeccioná el archivo sin credenciales:

```sh
molejoctl cluster registry init \
  --host registry.example.com \
  --namespace mi-namespace \
  --secret-name application-registry \
  --probe-image registry.example.com/apps/probe@sha256:<digest> \
  --output registry-setup.yaml

molejoctl cluster registry plan \
  --kube-context mi-cluster \
  --file registry-setup.yaml \
  --from-docker-config ~/.docker/config.json
```

Después de revisar el plan, aplicalo y verificá el resultado:

```sh
molejoctl cluster registry apply \
  --kube-context mi-cluster \
  --file registry-setup.yaml \
  --from-docker-config ~/.docker/config.json \
  --yes

molejoctl cluster registry verify --kube-context mi-cluster --file registry-setup.yaml
molejoctl cluster registry smoke --kube-context mi-cluster --file registry-setup.yaml
```

`apply` filtra el Docker config para el registry seleccionado, crea un Secret
`kubernetes.io/dockerconfigjson` administrado y agrega su nombre a la
ServiceAccount existente. Nunca crea el namespace ni la ServiceAccount y no
sustituye un Secret que no le pertenece. `smoke` usa `imagePullPolicy: Always` y
siempre elimina su Pod efímero.

Las credenciales pueden entrar por stdin con `--from-docker-config -`; nunca
deben estar en `RegistrySetup`, argumentos o Git. Usá credenciales pull-only y
rotalas ejecutando nuevamente `plan`, `apply` y `smoke`.

Este runbook atiende solamente imágenes de aplicaciones. Las credenciales OCI
de Helm y las imágenes internas de Molejo son flujos separados. Un namespace
creado posteriormente requiere otra ejecución explícita o un mecanismo continuo
elegido por el operador del cluster.
