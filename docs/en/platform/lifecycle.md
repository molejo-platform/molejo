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
