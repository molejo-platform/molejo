# Capacidad de Gateway para K3s

`GatewaySetup` es una receta local y versionada de `molejoctl`. No es una CRD de
Kubernetes y el control plane no la almacena. El perfil inicial `k3s` administra
una release fijada de Traefik, una `GatewayClass` y el `Gateway` HTTPS compartido
por las rutas de Molejo. DNS, firewall, proxy reverso y load balancer quedan fuera
de este contrato.

Los requisitos son un contexto K3s accesible, los componentes de clúster de
Molejo con las CRD necesarias de Gateway API y un Secret TLS existente en el
namespace del Gateway. Generá, inspeccioná, aplicá y verificá el setup con:

```sh
molejoctl capability gateway init \
  --profile k3s \
  --domain molejo.dev \
  --certificate-secret molejo-system/molejo-dev-tls \
  --output gateway-setup.yaml

molejoctl capability gateway plan --kube-context molejo-k3s --file gateway-setup.yaml
molejoctl capability gateway apply --kube-context molejo-k3s --file gateway-setup.yaml --yes
molejoctl capability gateway verify --kube-context molejo-k3s --file gateway-setup.yaml
```

El archivo generado puede versionarse porque no contiene credenciales. `plan` y
`verify` son de solo lectura. `apply` administra únicamente la release Traefik
generada y el Gateway compartido identificado por label; el comando rechaza
adoptar un Gateway, GatewayClass o NodePort de otro componente.

En una instalación nueva del control plane, conectá la consola al Gateway con:

```sh
molejoctl platform control-plane install \
  --kube-context molejo-k3s \
  --public-host cloud.molejo.dev \
  --gateway molejo-system/molejo \
  --gateway-section https-molejo
```

El `HTTPRoute` resultante expone solamente `console-web`. La consola reenvía
`/api/*` a la API interna del control plane.

La receta `config.molejo.dev/v1alpha2` requiere `instance.listeners` con 1–10
entradas. Cada una declara `name`, `hostname` exacto o wildcard y
`certificateSecret`. `init` genera apex (`https-apex`) y wildcard
(`https-molejo`). Las recetas anteriores se rechazan; regenerá en una instalación
alpha limpia. El Secret debe cubrir todos los nombres. Los listeners administrados
permiten HTTPRoutes solo desde namespaces con
`platform.molejo.dev/http-publication=enabled`, asignada por el provisionamiento
de Workspace y la instalación del Control Plane.

`verify` comprueba conformidad con la receta administrada. La biblioteca separada
`InspectConsumption` evalúa listener, attachment, soporte HTTPRoute y condiciones
actuales del Gateway externo mediante lectura, sin Helm ni Secret. El comando
integrado con APIs de bindings corresponde a la Fase 3. El Gateway externo
conectado no recibe patches; el Operator sigue administrando sus HTTPRoutes.

Consultá la [base de publicación HTTP](../../en/architecture/http-publication.md)
para límites, versiones, evidencia de estado y prueba TLS local.
