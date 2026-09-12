# Molejo conformance runner

`molejo-conformance` executes versioned product journeys against a Molejo Control
Plane and records a machine-readable verdict. It is intended for maintainers,
release automation, and infrastructure operators validating an installation.

The runner owns the meaning of a profile. Environment-specific harnesses own
cluster creation, platform installation, port forwards, fixture images, and
external prerequisites. This keeps the same profile usable against disposable
Kind clusters and pre-existing test clusters.

Read [DEVELOPMENT.md](DEVELOPMENT.md) before changing the runner or adding a
profile. The [testing guide](../../testing/README.md) maps the surrounding
harnesses and their mutation boundaries.

## Commands

```text
molejo-conformance profile list
molejo-conformance plan [options]
molejo-conformance run [options]
molejo-conformance cleanup [options]
```

- `profile list` prints every compiled profile and version without contacting a
  cluster.
- `plan` validates the supplied target and profile configuration and describes
  the intended effects without authenticating or making changes.
- `run` verifies target identity, executes the profile, writes evidence after
  every state change, and attempts cleanup on success, failure, or interruption.
- `cleanup` resumes cleanup from a previous run's private resource ledger after
  verifying that the requested endpoint and cluster match the recorded target.

Run the command from the repository root during development:

```bash
go -C tools run ./cmd/molejo-conformance profile list
```

Release and CI harnesses build the command with the runner version and source
revision embedded in the evidence.

## Compiled profiles

| Profile | Current guarantee |
| --- | --- |
| `alpha-core/v1` | Authentication, immutable application deployment, runtime readiness, current observability, withdrawal, tombstone behavior, and cleanup. |
| `http-publication/v1` | One application published through an Exact hostname and a SubdomainPool hostname on the same port, real TLS verification, partial address removal, withdrawal, and cleanup. |

A profile version and fingerprint identify the executed contract. Infrastructure
prerequisites such as platform installation, storage, metrics, Gateway, Registry,
DNS, and public certificate authority health remain separate checks.

## Choose the target mode

Use a disposable target when the harness owns the entire cluster lifecycle. The
runner may create a Workspace and, for HTTP publication, may manage a temporary
publication binding:

```text
--disposable-target
--publication-manage-binding
```

Use a persistent target for a pre-existing test cluster. It requires an existing
Workspace and uses the active publication binding without replacing it:

```text
--workspace-id <test-workspace-id>
```

`--publication-manage-binding` is rejected for persistent targets. The runner
still creates and removes its own projects, environments, applications, domains,
grants, and deployments inside the supplied test boundary.

Always provide `--cluster-id`. Provide `--cluster-uid` when it is known so the
runner can reject a different Kubernetes cluster registered under stale local
inputs. `--kube-context` is recorded as evidence; the Go runner itself talks to
the Control Plane API and does not invoke `kubectl`.

## Disposable Kind journey

The maintained complete journey creates an isolated Kind cluster and local
Registry, installs Molejo twice to prove installer idempotency, and runs both
compiled profiles:

```bash
just molejo-conformance kind
```

The harness requires Docker, Helm, OpenSSL, `jq`, and `kubectl`. It creates a
dedicated kubeconfig and never reads the current Kubernetes context. By default,
evidence is written to a private temporary directory whose path is printed at the
end. Set a parent directory when evidence must survive host cleanup:

```bash
MOLEJO_CONFORMANCE_OUTPUT_DIR="$PWD/.conformance-results" \
  just molejo-conformance kind
```

The harness does not remove this persistent output, and the example directory is
not covered by a repository ignore rule. Keep it private, do not commit it, and
remove it after retaining the required CI or release evidence.

## Plan and run against an existing cluster

Prepare a test Workspace, an HTTPS Control Plane endpoint, the Control Plane CA,
the owner password in a private file, and a fixture image pinned by digest. Then
inspect the plan:

```bash
go -C tools run ./cmd/molejo-conformance plan \
  --profile alpha-core \
  --cluster-id "$CLUSTER_ID" \
  --cluster-uid "$CLUSTER_UID" \
  --kube-context "$KUBE_CONTEXT" \
  --workspace-id "$WORKSPACE_ID"
```

Execute the same target only after reviewing that plan:

```bash
go -C tools run ./cmd/molejo-conformance run \
  --profile alpha-core \
  --endpoint "$CONTROL_PLANE_ENDPOINT" \
  --ca-file "$CONTROL_PLANE_CA_FILE" \
  --password-file "$OWNER_PASSWORD_FILE" \
  --image "$FIXTURE_IMAGE_BY_DIGEST" \
  --output "$RUN_DIRECTORY" \
  --cluster-id "$CLUSTER_ID" \
  --cluster-uid "$CLUSTER_UID" \
  --kube-context "$KUBE_CONTEXT" \
  --workspace-id "$WORKSPACE_ID"
```

`--output` starts a new ownership ledger and must not already contain
`report.json`. The runner rejects reuse so an interrupted run cannot be replaced
before its resources are recovered. Use a new directory for another run, or run
`cleanup --run-dir` against the existing directory.

The default TLS server name and HTTP `Host` match the in-cluster Control Plane
service. Use `--server-name` and `--host` when an external endpoint terminates or
routes TLS differently. Mutating requests send the default trusted origin
`http://127.0.0.1:8080`; use `--origin` only when the target installation has a
different configured trusted origin.

The HTTP publication profile additionally requires:

```text
--publication-gateway-namespace <namespace>
--publication-gateway-name <name>
--publication-exact-host <hostname>
--publication-pool-domain <base-domain>
--publication-pool-label <label>
--publication-exact-listener <listener-name>
--publication-pool-listener <listener-name>
--publication-probe-address <host:port>
--publication-ca-file <path>
```

The exact and pool listener hostnames default to the Exact hostname and
`*.<pool-domain>`. Set `--publication-exact-listener-hostname` or
`--publication-pool-listener-hostname` when the existing Gateway listener uses a
different compatible hostname. `--publication-probe-address` selects the socket
reached by the test while the requested hostname remains the HTTPS SNI and HTTP
host.

## Evidence and exit status

Every profile output directory is created with mode `0700`. Its files use mode
`0600`:

| File | Meaning |
| --- | --- |
| `report.json` | Authoritative incremental verdict, profile fingerprint, target identity, assertions, outputs, ownership ledger, and cleanup result. |
| `junit.xml` | CI projection generated from the same report snapshot. It is not an independent verdict. |

Runner statuses are `PASS`, `FAIL`, `BLOCKED`, `PENDING`, and `SKIPPED`. Process
exit codes are:

| Code | Meaning |
| --- | --- |
| `0` | The requested operation completed and the profile passed. |
| `1` | The profile, assertion, or cleanup failed. |
| `2` | Usage is invalid, a prerequisite blocks execution, or durable evidence cannot be produced. |

Read `report.json` before interpreting JUnit or console progress. `PASS` lines on
stdout are incremental observations; only the final report is the verdict.

## Recover cleanup

If a run retains conformance-owned resources, keep its result directory intact
and retry with credentials for the same target:

```bash
go -C tools run ./cmd/molejo-conformance cleanup \
  --run-dir "$RUN_DIRECTORY" \
  --endpoint "$CONTROL_PLANE_ENDPOINT" \
  --ca-file "$CONTROL_PLANE_CA_FILE" \
  --password-file "$OWNER_PASSWORD_FILE"
```

Cleanup rejects a different endpoint or cluster identity and only follows
allowlisted API paths recorded for the same run. Inspect `.cleanup` and
`.resources` in `report.json` if recovery does not complete. Do not edit the
ledger to force deletion; investigate the target mismatch or remove the resources
through the owning product workflow.

## Related checks

The compiled profiles do not provision cloud infrastructure or validate every
installation prerequisite. Use the scripts described in
[tools/testing/README.md](../../testing/README.md) for:

- installation, storage, metrics, Gateway, TLS, and private Registry checks on a
  named Kubernetes context;
- read-only public acceptance for `molejo.dev`, the Console, and the Registry;
- disposable end-to-end execution in Kind.

The scheduled and branch workflow is defined in
[`.github/workflows/conformance.yml`](../../../.github/workflows/conformance.yml).
