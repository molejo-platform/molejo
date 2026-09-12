# Developing the Molejo conformance runner

This guide defines the local development contract for `molejo-conformance`.
Repository-wide setup, code style, commits, and quality gates remain authoritative
in [`docs/en/CONTRIBUTING.md`](../../../docs/en/CONTRIBUTING.md).

## Architectural boundary

The runner follows two complementary constraints:

- Functional Core, Imperative Shell: keep comparisons, validation, planning,
  status derivation, fingerprints, and cleanup authorization deterministic. Keep
  HTTP, files, time, signals, process lifecycle, Kubernetes, and external tools at
  explicit edges.
- Screaming Architecture: organize profiles around Molejo product journeys and
  observable guarantees. Kubernetes resources and harness mechanics support those
  journeys but do not define the public product contract.

The current ownership map is:

| Area | Responsibility |
| --- | --- |
| `cmd/molejo-conformance` | CLI parsing, process signals, secret-file loading, exit-code mapping, and progress output. |
| `internal/conformance/catalog.go` | Compiled profile catalog and profile versions. |
| `internal/conformance/runner.go` | Target/profile validation, plan rendering, scenario sequencing, and cleanup orchestration. |
| `internal/conformance/journey.go` | Core application lifecycle journey. |
| `internal/conformance/publication.go` | Exact and SubdomainPool HTTP publication journey and TLS observations. |
| `internal/conformance/client.go` | Authenticated Control Plane HTTP effects. |
| `internal/conformance/report.go` | Incremental private evidence and JUnit projection. |
| `internal/conformance/cleanup.go` | Ownership-ledger validation and bounded cleanup. |
| `testing/*.sh` | Environment assembly, prerequisite checks, process lifecycle, diagnostics, and external acceptance. |

Do not move product assertions into shell to make a harness pass. Do not add
cloud-provider or Kubernetes provisioning logic to a compiled profile. A harness
may satisfy prerequisites, but the profile must retain the same identity and
meaning across supported substrates.

## Add or change a profile

1. State the user journey and observable guarantee. A profile is justified when
   the behavior crosses components and cannot be proved adequately by narrower
   tests.
2. Add or change the profile ID and version in `model.go`, then register the
   compiled profile in `catalog.go`.
3. Implement each scenario around Molejo API behavior. Put deterministic
   comparison and transition decisions in small functions; call HTTP, TLS, and
   time-dependent operations through explicit edge functions.
4. Add assertions only for externally meaningful outcomes. Assertion text must
   describe observed evidence rather than implementation steps.
5. Register every created resource in the reporter as soon as the API confirms
   ownership and before a later effect can make cleanup necessary.
6. Extend the cleanup allowlist and its negative tests when introducing a new
   resource kind. A new ledger kind without cleanup authorization is incomplete.
7. Add the narrow unit and HTTP tests required for validation, comparison,
   reporting, pagination, retry, and cleanup behavior.
8. Update the profile table and operational examples in `README.md`. Update the
   Kind harness only when the profile requires a new local prerequisite.

Profiles are compiled on purpose. Do not load scenario definitions, arbitrary API
paths, cleanup paths, or expected verdicts from YAML or JSON. Infrastructure setup
files may configure a harness or `molejoctl` capability, but they cannot redefine
a Molejo conformance guarantee.

## Scenario and polling rules

- Give every scenario a stable ID, description, required flag, and bounded
  timeout.
- Use the scenario context deadline for all effects. Add a shorter child deadline
  only when the operation has a distinct bound, such as the live-log probe.
- Poll an observable state through `await`; retry only temporary unavailability.
  Return terminal failure states immediately.
- Use bounded API page sizes and follow opaque cursors. Tests must cover multiple
  pages whenever collection affects a verdict.
- Preserve idempotency keys for retried mutations and conditional versions for
  state transitions and cleanup.
- Do not use fixed sleeps as readiness assertions.

A scenario failure must retain the original cause. Diagnostic or cleanup errors
may be joined to it, but must not replace it.

## Target and ownership rules

Target verification occurs before scenario effects. Preserve these invariants:

- the Molejo cluster ID must match the API response;
- the cluster must be `Active`;
- an expected Kubernetes cluster UID must match when provided;
- persistent targets require a pre-existing test Workspace;
- binding management is allowed only when the complete target is disposable.

The evidence ledger is an authorization boundary for recovery, not a general API
client. Cleanup runs in reverse registration order and accepts only known resource
kinds, paths, identifiers, headers, and the current run ID. New cleanup behavior
must include tests proving rejection of foreign hosts, paths, IDs, run IDs,
headers, and resources.

Creating a run claims `report.json` exclusively. Never overwrite or resume an
existing ledger through `run`; use `cleanup --run-dir` to recover it. Cleanup
must distinguish synchronous deletion from an accepted asynchronous operation.
It may mark an `AppEnvironment` archived only after its recorded idempotent
operation reaches `Succeeded`.

Never infer ownership from a name prefix alone when an API identity, revision, or
recorded relationship is available. Never put pre-existing resources in the
cleanup ledger.

## Evidence contract

`report.json` is the canonical result and is rewritten atomically after every
meaningful transition. `junit.xml` is derived from the same in-memory snapshot.

When changing evidence:

- keep the report self-describing with schema, runner, revision, run, profile,
  fingerprint, and target identity;
- derive coverage and CI status from scenario state in one place;
- preserve private directory mode `0700` and file mode `0600`;
- store hashes, certificate identity, timestamps, and normalized diagnostics
  instead of response bodies or credentials;
- bump `ReportSchemaVersion` when consumers can no longer interpret the previous
  structure;
- bump a profile version when its required journey or pass criteria change;
- add report and JUnit tests before changing their serialized meaning.

Alpha versions may break without migrations, but a breaking change still needs a
new explicit version so stored evidence says what was executed.

## Harness rules

Shell harnesses may create a disposable cluster, install dependencies, build
fixtures, invoke the runner, collect diagnostics, and destroy what they own. They
must:

- use a dedicated kubeconfig or require an explicit named context;
- declare whether each mode is read-only, creates temporary resources, or changes
  installation state;
- pin images and dependencies that define the test environment;
- remove scratch credentials and infrastructure on success, failure, and signals;
- retain only sanitized evidence in the published result directory;
- report incomplete teardown as failure;
- treat command or decoding errors as observation failures, never as proof that
  a resource is absent;
- preserve runner JSON as the profile verdict instead of parsing console text.

External DNS and public certificate checks belong to a separate acceptance tool.
Cloud or machine provisioning belongs to an infrastructure adapter that prepares
a target and then invokes the same runner or prerequisite checks.

## Validation workflow

Run focused tests first:

```bash
GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" \
  go -C tools test ./cmd/molejo-conformance/... ./internal/conformance/...
just lint
just script-check
```

Then run repository-owned distribution checks:

```bash
just distribution-test
just script-check
just quality-report
```

The quality report is informational while the alpha baseline is reduced. It
reports complex or long production functions and low maintainability without a
hard file-size limit. Treat each finding as a prompt to inspect cohesion and
decision boundaries, not as an instruction to fragment code.

Run the disposable integration proof when runner behavior, environment assembly,
installation, Gateway/TLS wiring, fixture images, cleanup, or evidence publication
changes:

```bash
just molejo-conformance kind
```

The Kind run requires Docker and may take tens of minutes. A documentation-only
change does not require it when commands and contracts were verified statically.

Before review, confirm that:

- the plan states every new class of effect;
- the profile version matches its semantics;
- failure, interruption, and cleanup remain bounded;
- persistent targets cannot acquire disposable privileges;
- JSON and JUnit agree;
- no artifact retains secret material;
- the operational and testing guides match the executable interface.
