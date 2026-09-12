# Molejo test and acceptance harnesses

This directory contains environment-specific shells around Molejo commands and
the compiled [conformance runner](../cmd/molejo-conformance/README.md). Each tool
has a distinct mutation boundary. Select the narrowest tool that proves the
required behavior.

## Harness matrix

| Entry point | Target | Mutation boundary | Evidence or result |
| --- | --- | --- | --- |
| `molejo-conformance.sh kind` | New local Kind cluster | Creates and destroys its own cluster, Registry, images, installation, and fixtures. Never uses the current context. | Harness JSON/JUnit, profile JSON/JUnit, and sanitized failure diagnostics. |
| `molejo-conformance.sh installation` | Named existing context | Read-only verification of the current Control Plane, Agent pairing, and `molejoctl platform doctor`. | Console verdict. |
| `molejo-conformance.sh metrics-current` | Named existing context | Read-only Metrics API request. | Console verdict. |
| `molejo-conformance.sh storage-rwo` | Named existing context | Runs the bounded `molejoctl` storage smoke using the selected StorageClass. | Console verdict and command-managed temporary resources. |
| `molejo-conformance.sh publication-binding` | Named existing context | Read-only verification of the Gateway setup file. | Console verdict. |
| `molejo-conformance.sh registry-private` | Named existing context | Verifies Registry configuration and runs its bounded private-pull smoke. | Console verdict and command-managed temporary resources. |
| `control-plane-k3s.sh` | Named existing context | `verify` is read-only; `install`, `teardown`, and `cycle` change installation and Agent identity state. | Console verdict. |
| `tls-k3s.sh verify` | Named existing context | Read-only verification of a TLS setup file. | Console verdict. |
| `public-edge-acceptance.sh` | Public HTTPS endpoints | Read-only network probes; never invokes `kubectl` or changes DNS, routes, or certificates. | Private JSON evidence with hashes and certificate identity. |

## Common entry point

Use the `just` recipe to run the disposable journey or one prerequisite profile:

```bash
just molejo-conformance kind
just molejo-conformance installation context=molejo-k3s
just molejo-conformance metrics-current context=molejo-k3s
just molejo-conformance storage-rwo context=molejo-k3s storage_class=local-path
just molejo-conformance publication-binding context=molejo-k3s gateway_file=./gateway-setup.yaml
just molejo-conformance registry-private context=molejo-k3s registry_file=./registry-setup.yaml
```

`MOLEJOCTL_BIN` may point the existing-context scripts at a prebuilt binary.
Otherwise they run `go run ./apps/molejoctl` from the repository.

## Persistent K3s checks

Verify the installation without changing it:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --file ./tls-setup.yaml
```

Installation requires `--version` or `MOLEJO_VERSION`. Destructive lifecycle
modes require the context name twice so an accidental argument cannot select a
different context silently:

```bash
tools/testing/control-plane-k3s.sh teardown \
  --context molejo-k3s \
  --confirm molejo-k3s

tools/testing/control-plane-k3s.sh cycle \
  --context molejo-k3s \
  --version 0.1.0-alpha.4 \
  --confirm molejo-k3s
```

These commands still require explicit authorization before changing a shared or
external cluster. `--confirm` guards target selection; it does not grant that
authorization.

## Public edge acceptance

The public probe verifies these defaults:

- `https://molejo.dev/` returns `200`;
- `https://cloud.molejo.dev/` returns `200`;
- `https://registry.molejo.dev/v2/` returns `401` with a Bearer challenge;
- every served certificate remains valid for at least 14 days.

Run it with a private output directory:

```bash
tools/testing/public-edge-acceptance.sh \
  --output ./public-edge-evidence \
  --apex-body-marker '<expected marker>' \
  --console-body-marker '<expected marker>'
```

The report retains the HTTP status, body SHA-256, required-header result,
certificate SHA-256, and certificate expiration. It does not retain response
bodies, certificates, headers, or credentials. Use `--apex-host`,
`--console-host`, and `--registry-host` for another environment, and
`--minimum-certificate-days` to change the validity threshold.

## Disposable Kind evidence

`kind-conformance.sh` builds immutable local fixture and component images, installs
the packaged charts, verifies namespace authorization boundaries, runs
`alpha-core/v1` and `http-publication/v1`, and tears down its Docker and Kind
resources on every exit.

Set `MOLEJO_CONFORMANCE_OUTPUT_DIR` to retain evidence under a known parent. Each
run creates a private child directory containing:

```text
harness-report.json
harness-junit.xml
results/report.json
results/junit.xml
publication-results/report.json
publication-results/junit.xml
diagnostics/                 # failure only
```

Scratch kubeconfigs, passwords, private keys, certificates, binaries, build
archives, and Registry state live elsewhere and are deleted. The harness fails if
it cannot confirm teardown. JSON is authoritative and is published only after its
derived JUnit file is durable.

## Safety rules

- Never depend on the current Kubernetes context. Pass `--context`, or let the
  Kind harness create a dedicated kubeconfig.
- Inspect a setup file before passing it to `molejoctl`; these scripts do not turn
  an untrusted file into a safe target.
- Keep output directories private even when reports are designed to be sanitized.
- Treat storage and Registry smokes as mutations with temporary resources.
- Keep public acceptance read-only. Exercise domain, grant, withdrawal, and
  cleanup mutations in a disposable target or explicitly authorized test scope.
- Run `bash -n tools/testing/*.sh` after editing any harness.

Development rules for profiles, evidence, cleanup, and future infrastructure
adapters are in
[`cmd/molejo-conformance/DEVELOPMENT.md`](../cmd/molejo-conformance/DEVELOPMENT.md).
