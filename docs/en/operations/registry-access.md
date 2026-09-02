# Application registry access

`RegistrySetup` is a local, versioned `molejoctl` runbook. It prepares one
existing namespace to pull application images from a private registry. It is
not a Kubernetes CRD, is not stored by the control plane, and does not configure
nodes, IAM, container runtimes, DNS, or network routes.

Generate and inspect the credential-free setup file:

```sh
molejoctl cluster registry init \
  --host registry.example.com \
  --namespace my-application-namespace \
  --secret-name application-registry \
  --probe-image registry.example.com/apps/probe@sha256:<digest> \
  --output registry-setup.yaml

molejoctl cluster registry plan \
  --kube-context my-cluster \
  --file registry-setup.yaml \
  --from-docker-config ~/.docker/config.json
```

After reviewing the plan, apply and verify it:

```sh
molejoctl cluster registry apply \
  --kube-context my-cluster \
  --file registry-setup.yaml \
  --from-docker-config ~/.docker/config.json \
  --yes

molejoctl cluster registry verify --kube-context my-cluster --file registry-setup.yaml
molejoctl cluster registry smoke --kube-context my-cluster --file registry-setup.yaml
```

`apply` filters the Docker config to the selected registry, creates an owned
`kubernetes.io/dockerconfigjson` Secret, and appends its name to the existing
ServiceAccount. It never creates the namespace or ServiceAccount and refuses to
overwrite an unowned Secret. `smoke` uses `imagePullPolicy: Always` and always
removes its ephemeral Pod.

Credentials may be read from stdin with `--from-docker-config -`; they never
belong in `RegistrySetup`, command arguments, or version control. Use pull-only
credentials. Rotate them by running `plan`, `apply`, and `smoke` again.

This runbook manages application image pulls only. Helm OCI credentials and the
credentials used by Molejo's own components are separate. A namespace created
later requires an explicit runbook execution or a continuous cluster mechanism
chosen by its operator.
