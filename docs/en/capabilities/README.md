# Cluster capabilities

Capabilities connect infrastructure selected by the cluster operator to Molejo.
Every capability has explicit ownership:

- `external`: Molejo only consumes the result;
- `runbook-managed`: `molejoctl` applies a reviewed local recipe;
- `molejo-managed`: the resource is part of the Molejo release;
- `provider-managed`: the Kubernetes or cloud provider owns its lifecycle.

Current runbooks are [Gateway with Traefik](gateway-traefik.md),
[TLS with cert-manager](tls-cert-manager.md), [registry access](registry.md), and
[Kubernetes storage verification](storage.md).
They use `init`, `plan`, `apply`, `verify`, and, when meaningful, `smoke`. A runbook
is not a plugin API, does not become a control-plane resource, and never transfers
provider credentials to the Platform Operator or Cluster Agent.
