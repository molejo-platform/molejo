# K3s day-zero setup

`ClusterSetup` is a local, versioned `molejoctl` recipe. It is not a Kubernetes
CRD and is not stored by the control plane. The initial `k3s` profile manages a
pinned Traefik release, one `GatewayClass`, and the shared HTTPS `Gateway` used by
Molejo routes. DNS, firewall, reverse proxy, and load-balancer configuration stay
outside this contract.

Prerequisites are a reachable K3s context, Molejo cluster components that include
the required Gateway API CRDs, and an existing TLS Secret in the Gateway
namespace. Generate, inspect, apply, and verify the setup with:

```sh
molejoctl cluster setup init \
  --profile k3s \
  --domain molejo.dev \
  --certificate-secret molejo-system/molejo-dev-tls \
  --output cluster-setup.yaml

molejoctl cluster setup plan --kube-context molejo-k3s --file cluster-setup.yaml
molejoctl cluster setup apply --kube-context molejo-k3s --file cluster-setup.yaml --yes
molejoctl cluster setup verify --kube-context molejo-k3s --file cluster-setup.yaml
```

The generated file is safe to keep in version control because it contains no
credentials. `plan` and `verify` are read-only. `apply` owns only the generated
Traefik Helm release and the labeled shared Gateway; it refuses to adopt a
foreign Gateway, GatewayClass, or occupied NodePort.

For a fresh control-plane installation, attach the console to that Gateway with:

```sh
molejoctl control-plane install \
  --kube-context molejo-k3s \
  --public-host cloud.molejo.dev \
  --gateway molejo-system/molejo \
  --gateway-section https-molejo
```

The resulting `HTTPRoute` exposes only `console-web`. The console proxies
`/api/*` to the internal control-plane API.
