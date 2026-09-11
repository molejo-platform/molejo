# Gateway capability for K3s

`GatewaySetup` is a local, versioned `molejoctl` recipe. It is not a Kubernetes
CRD and is not stored by the control plane. The initial `k3s` profile manages a
pinned Traefik release, one `GatewayClass`, and the shared HTTPS `Gateway` used by
Molejo routes. DNS, firewall, reverse proxy, and load-balancer configuration stay
outside this contract.

Prerequisites are a reachable K3s context, Molejo cluster components that include
the required Gateway API CRDs, and an existing TLS Secret in the Gateway
namespace. Generate, inspect, apply, and verify the setup with:

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

The `config.molejo.dev/v1alpha2` recipe has `instance.listeners` (1–10 entries).
Each entry declares `name`, an exact or wildcard `hostname`, and
`certificateSecret`. `init` emits both apex (`https-apex`) and wildcard
(`https-molejo`) listeners. Old single-listener recipes are rejected; regenerate
on a clean alpha installation. The Secret must cover every configured name.
The managed listeners allow HTTPRoutes only from namespaces labeled
`platform.molejo.dev/http-publication=enabled`, assigned by Molejo namespace
provisioning and control-plane installation.

The generated file is safe to keep in version control because it contains no
credentials. `plan` and `verify` are read-only. `apply` owns only the generated
Traefik Helm release and the labeled shared Gateway; it refuses to adopt a
foreign Gateway, GatewayClass, or occupied NodePort.

For a fresh control-plane installation, attach the console to that Gateway with:

```sh
molejoctl platform control-plane install \
  --kube-context molejo-k3s \
  --public-host cloud.molejo.dev \
  --gateway molejo-system/molejo \
  --gateway-section https-molejo
```

The resulting `HTTPRoute` exposes only `console-web`. The console proxies
`/api/*` to the internal control-plane API.

`verify` checks conformity with this managed recipe, including its prerequisites.
The independent `InspectConsumption` library evaluates an external Gateway's
listener, attachment, HTTPRoute support and current conditions with read-only
credentials, without Helm or Secret access. The integrated command using that
inspection and product binding APIs belongs to phase three. A connected external
Gateway is never patched; the Operator still manages its own HTTPRoutes.

See [HTTP publication foundation](../architecture/http-publication.md) for limits,
versioning, status evidence and the local TLS conformance command.
