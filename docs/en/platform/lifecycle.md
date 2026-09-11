# Platform lifecycle

Platform lifecycle owns only Molejo components and their product contracts:

```sh
molejoctl platform runtime install --kube-context <context> --version <version>
molejoctl platform control-plane install --kube-context <context> --version <version>
molejoctl platform doctor --kube-context <context>
molejoctl platform status --kube-context <context>
```

The runtime is the Platform Operator and outbound Cluster Agent. The control plane
is the API, Console, PostgreSQL configuration, and Agent pairing material installed
by the current alpha chart. `doctor` and `status` validate the runtime contract.

Alpha releases intentionally do not promise in-place upgrades. If an installed
release or its immutable public configuration differs, back up any data that must
be retained, follow the experimental teardown, and reinstall the requested alpha.

## Local charts for development

Both `install` commands accept `--chart-path` to run the same flow with an already
prepared local directory or `.tgz` archive. The chart must use API `v2`, have the
expected name (`molejo-cluster` or `molejo-control-plane`), and carry the same
version passed through `--version`.

The directories under `deploy/charts` are release-tool inputs and are not directly
installable: the prepared artifact also contains the manifests, CRDs, and image
digests generated during packaging. Local charts are treated as immutable; to test
different content under the same alpha version, remove the experimental
installation and reinstall it.

## HTTP publication administration

Control Plane installation prints the non-secret Cluster ID and live Kubernetes
UID. Save the Cluster ID in a versioned setup; keep the Kubernetes context explicit
so molejoctl can fence a recreated or wrong cluster before product mutation.

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

Run the read-only plan, inspect its operations, then explicitly apply and verify:

```sh
molejoctl capability publication plan --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml
molejoctl capability publication apply --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml --yes
molejoctl capability publication verify --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml
```

Use `--ca-file` for a private Control Plane CA. Credentials are requested only from
an interactive terminal. `status` and `dependents` read the same API state; lists
return one bounded page and an explicit cursor. Removal is never inferred from the
setup file. Use the separate `grant revoke`, `domain delete`, and
`binding disconnect` commands after removing Desired, Applied and Executable
references. There is no force option.

The binding declares existing Gateway listeners. It does not install a Gateway,
issue or renew certificates, edit DNS, or prove public reachability. Those remain
with the configured infrastructure owners; private DNS names are valid inputs when
the declared routing and grants are valid.
