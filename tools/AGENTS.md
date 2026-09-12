# Molejo tooling instructions

These instructions extend the repository root `AGENTS.md` for release,
conformance, generation, and test-support tooling.

## Read by task

- Conformance operation: [`cmd/molejo-conformance/README.md`](cmd/molejo-conformance/README.md)
- Conformance development: [`cmd/molejo-conformance/DEVELOPMENT.md`](cmd/molejo-conformance/DEVELOPMENT.md)
- Test harness ownership and safety: [`testing/README.md`](testing/README.md)
- Release work: [`cmd/release/README.md`](cmd/release/README.md)

## Conformance boundaries

- Keep profiles compiled and versioned. They define Molejo product journeys;
  harnesses only prepare infrastructure and invoke them.
- Keep product decisions in Go. Shell owns environment assembly, process
  lifecycle, tool invocation, and diagnostic collection.
- Treat `report.json` as the authoritative verdict. Generate JUnit from the same
  report state rather than implementing a second verdict path.
- Record a resource in the private ownership ledger before later effects depend
  on its cleanup. Cleanup may operate only on allowlisted resources owned by the
  same run.
- Require an existing Workspace for persistent targets. Never manage a
  persistent target's publication binding from a conformance profile.
- Keep external DNS, certificate authority, cloud provisioning, and public-edge
  acceptance outside compiled application profiles.

## Safety and evidence

- Never retain passwords, tokens, private keys, certificates, or kubeconfigs in
  published evidence. Use private scratch space and remove it on every exit.
- Never use the current Kubernetes context implicitly. Require and record the
  selected context or use an isolated kubeconfig created by a disposable harness.
- Do not mutate an external cluster, DNS provider, registry, or cloud account
  without explicit authorization for that operation.
- Preserve bounded timeouts and pagination. Do not add fixed sleeps when an
  observable readiness condition exists.
- Diagnostic collection must be best-effort, sanitized, and unable to replace
  the original failure or verdict.

## Validation

Run the narrowest relevant checks while iterating:

```bash
GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" \
  go -C tools test ./cmd/molejo-conformance/... ./internal/conformance/...
just lint
just script-check
```

Run `just distribution-test` and `just script-check` before completing a change
that crosses the runner and its harnesses. Run the Kind harness only when its
Docker, cluster, installation, and cleanup behavior is part of the change.
Use `just quality-report` to inspect maintainability pressure. It is diagnostic;
do not split a cohesive file merely to reduce its line count.
