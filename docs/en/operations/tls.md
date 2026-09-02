# Cluster TLS

`molejoctl cluster tls configure` validates or provisions certificate material and writes a reusable `ClusterTLSBinding`. It does not install a Gateway, publish applications, or manage application DNS records.

Use the external Secret example when certificate lifecycle is managed outside Molejo:

```bash
molejoctl cluster tls configure \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-existing-secret.yaml
```

The first run prints a plan. Apply it explicitly with `--yes`. A repeated run must report the profile as verified without requiring approval.

The `existing-secret` driver requires a `kubernetes.io/tls` Secret whose key matches the certificate, whose validity exceeds 24 hours, and whose SANs cover every configured domain.

The `cert-manager` driver supports ACME DNS-01 with Cloudflare in this first contract version. The referenced Secret must already exist in the `cert-manager` namespace with the API token under `api-token`. Molejo never stores the token in the profile or prints it. Start with the staging example in `deploy/examples/tls-cert-manager-cloudflare.yaml` before changing the issuer environment to `production`.

The resulting binding is stored as `molejo-system/molejo-tls-<profile>`. `molejoctl cluster doctor` validates every stored binding and its current TLS Secret.
