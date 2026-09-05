# ADR-0014: External CI Release and Deployment Boundary

Status: Accepted

## Context

Molejo must work with GitHub Actions, cloud-native pipelines, and future
in-cluster builders without making any one of them part of the product model.
An image existing in a registry is not yet a Molejo Release, and changing an
image in Kubernetes bypasses product authorization, audit, and reconciliation.

## Decision

CI owns source checkout, build, verification, and pushing an OCI image. It then
registers the immutable `repository@sha256:digest` as an app-scoped Release in
the control plane. A separate environment-scoped request selects that Release
for deployment. The control plane remains the authority for desired state and
the Cluster Agent continues to transport only versioned runtime commands to the
Platform Operator.

External automation authenticates as a first-class ServiceAccount Principal.
Its opaque credential is stored only as a hash, expires, can be revoked, and is
issued once. Grants are restricted to `release.write` for one App and
`deployment.create` for explicitly selected App Environments. Deployment keeps
the existing `If-Match` and idempotency contracts. An automation identity may
read its granted environment and only operations that it requested.

Release registration is provider-independent. Source and producer provenance
are descriptive metadata; GitHub is not a database relation. An idempotency key
replays the same payload and conflicts with a different payload. Equal digests
may be registered by different apps or provenance runs. Releases are immutable
history and are not expired by an implicit per-environment counter.

The managed BuildKit path remains an adapter that produces the same Release
record. Future workload identity such as GitHub OIDC may replace the opaque
credential without changing the Release or Deployment APIs.

## Consequences

Pipelines can compose with Molejo through a small stable HTTP boundary. Registry
authentication and image pulling remain Kubernetes/operator-runbook concerns;
the control plane only enforces its registry allowlist. Neither the Cluster
Agent nor the Platform Operator receives CI tokens or provider-specific code.

Creating and revoking automation credentials is initially restricted to
Workspace Owners or explicit Managers. Credential rotation is a create-update-
revoke operation performed by the pipeline operator.

## Alternatives Considered

Letting CI patch Kubernetes directly was rejected because it bypasses the
control-plane source of truth. Modeling GitHub workflow runs as Builds was
rejected because it couples Releases to one provider. Sending registry
credentials to the Agent or Operator was rejected because Kubernetes already
owns image-pull authentication.

## References

- [External CI operations](../operations/external-ci.md)
- [ADR-0013: Outbound Cluster Agent Identity and Pairing](0013-outbound-cluster-agent-identity-and-pairing.md)
