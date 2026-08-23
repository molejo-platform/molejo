# ADR-0002: Reconciliation State and Observability Contract

## Status

Draft

## Context

`AppDeployment` is an asynchronous contract. Applying its desired Deployment is
not equivalent to completing a rollout, and Kubernetes may report transient,
persistent, or semantic failures through different mechanisms. If those states
are interpreted inside I/O code, status semantics, retries, logs, and tests can
diverge as the operator evolves.

Operational diagnosis also requires correlated signals without binding the
operator to a particular monitoring or tracing backend.

## Decision

The operator will evaluate a pure internal state machine from an observed
snapshot. It produces exactly one active state: `Ready`, `Progressing`, or
`Degraded`. A rollout is ready only after the Deployment observed its generation,
all desired replicas are current and available, no old replicas remain, and no
replicas are unavailable. Deployment failure conditions affect the decision only
after the Deployment has observed its current generation; conditions from an
older generation are stale and the rollout remains `Progressing`.

The current public reasons are the closed set `DeploymentProgressing`,
`DeploymentAvailable`, `ProgressDeadlineExceeded`, `ReplicaFailure`,
`OwnershipConflict`, `ReconcileFailed`, `HTTPRouteProgressing`,
`HTTPRouteRejected`, `GatewayProgressing`, `GatewayRejected`, and
`HostnameConflict`. The publication-specific reasons extend the original workload
state contract and are defined by ADR-0004. Ownership conflicts are semantic
blocks: they update status, preserve the last successfully observed release, and
use a five-minute requeue without returning an error. Persistent Kubernetes API
errors use `ReconcileFailed` with a sanitized status message, preserve the
observed fields, and use the same bounded requeue. Transient Kubernetes API errors
return an error and use controller-runtime backoff. Failures reported by the
current Deployment status update Conditions without an artificial retry.

Conditions are the durable public state. Kubernetes Events explain meaningful
transitions. Structured logs provide technical detail, bounded Prometheus metrics
provide aggregation, and OpenTelemetry spans provide causal timing. Metrics are
served with TLS and Kubernetes authentication and authorization. OTLP trace
export is optional and configured through standard environment variables.
Technical error details remain in correlated logs and traces and are not copied
to the public status.

## Consequences

State semantics can be tested independently of Kubernetes I/O, and adapters can
change without redefining readiness. Public reasons, metric names, span names,
and label cardinality become compatibility-sensitive contracts.

The operator gains OpenTelemetry and Kubernetes delegated authentication
dependencies. Trace export failures must not interrupt reconciliation, and
operators must explicitly grant consumers access to `/metrics`.

## Alternatives Considered

Derive readiness only from available replicas. This was rejected because a
rolling update may still serve old replicas.

Return every degraded state as a reconciliation error. This was rejected because
persistent semantic blocks would create noisy hot retries.

Install a monitoring and tracing stack with the operator. This was rejected
because collection and storage backends are deployment concerns and are not
required by the platform contract.

## References

- [Kubernetes Deployment status](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#deployment-status)
- [Kubebuilder metrics protection](https://book.kubebuilder.io/reference/metrics)
- [OpenTelemetry Go exporters](https://opentelemetry.io/docs/languages/go/exporters/)
- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
