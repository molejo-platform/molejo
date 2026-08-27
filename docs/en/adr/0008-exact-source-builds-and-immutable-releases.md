# ADR-0008: Exact Source Builds and Immutable Releases

Status: Draft

## Context

A build executes an untrusted repository Dockerfile and crosses GitHub,
PostgreSQL, a builder, and an OCI registry. Branch names are mutable, credentials
must not enter the build context, and a successful command without a recorded
digest is not a reproducible product release.

## Decision

The API resolves the selected repository default branch to an exact 40-character
commit SHA before creating an idempotent Build. A separate worker claims Builds
with a lease and fencing token, obtains a short-lived GitHub installation token,
downloads the archive for that exact SHA, and safely extracts it into an
ephemeral directory. The first contract accepts only a regular `Dockerfile` at
the repository root and always targets `linux/amd64`.

The worker sends the context to a separate rootless BuildKit daemon over mutual
TLS. GitHub and registry credentials remain mounted only on the worker and are
never copied into the context or passed on its command line. The worker and
builder have explicit time, CPU, memory, and ephemeral-storage limits. This
pre-alpha topology runs one worker and one builder; it does not claim strong
multi-tenant isolation.

The pushed tag is the exact commit SHA. A Release is promoted transactionally
only after BuildKit returns a valid OCI digest, and stores the digest-pinned
image, commit, App, Build, and platform. Deployments created from a Release take
their image from the server-side Release; clients cannot replace that image in a
later update.

Buildpacks, monorepo path selection, build variables, build secrets, webhooks,
CI orchestration, cache contracts, SBOM, signing, and scanning remain outside
this decision.

## Consequences

The same Release always identifies the same source commit and image digest, and
a failed Build cannot become deployable. Build logs and bounded retry state are
available without persisting GitHub tokens. Rootless BuildKit on Kubernetes uses
`--oci-worker-no-process-sandbox` and unconfined seccomp/AppArmor as required by
the upstream deployment model; this is an explicit pre-alpha risk. Stronger
per-build isolation is required before production or hostile multi-tenant use.
