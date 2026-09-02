# Cluster TLS

Molejo owns the public-domain policy and consumes standard Kubernetes TLS Secrets. Certificate issuance and renewal remain owned by the cluster operator and the selected certificate controller. The control plane, platform operator, and cluster Agent never receive DNS-provider credentials.

`molejoctl cluster tls verify` is read-only and validates that the configured `kubernetes.io/tls` Secret has a matching key, remains valid for at least 24 hours, and covers every DNS name in a local `TLSSetup` document:

```bash
molejoctl cluster tls verify \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-existing-secret.yaml
```

`molejoctl cluster tls prepare` is an optional day-zero convenience recipe. The first recipe installs cert-manager and requests a Let's Encrypt certificate through Cloudflare DNS-01. cert-manager owns renewal after the command exits.

Run staging before production:

```bash
molejoctl cluster tls prepare \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-molejo-dev-staging.yaml \
  --credential-env TF_VAR_cloudflare_api_token \
  --yes

molejoctl cluster tls prepare \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-molejo-dev-production.yaml \
  --credential-env TF_VAR_cloudflare_api_token \
  --yes
```

The token is stored in `cert-manager/cloudflare-dns-token`, under the `api-token` key, and is never written to the setup file or command output. The token requires `Zone - DNS - Edit` and `Zone - Zone - Read` for `molejo.dev`. The Cloudflare account ID is not used by the cert-manager API-token solver.

Staging and production use separate Certificate resources and Secrets. The production outcome is `molejo-system/molejo-dev-tls`, covering `molejo.dev`, `*.molejo.dev`, and `*.stateful.molejo.dev`.

The `TLSSetup` file is a local molejoctl recipe, not a control-plane or workload API. It does not install a Gateway, attach the Secret to a listener, create permanent application DNS records, or persist a Molejo-specific TLS binding. Gateway API `certificateRefs` is the future consumption contract.
