# ADR-0003: Private Stateless Backend Runtime Contract

## Status

Draft

## Context

The first executable slice proved that an `AppDeployment` can manage a
Deployment. A useful private stateless backend also needs stable internal
networking, explicit runtime resources, health semantics, and a secure default
container profile. Copying Kubernetes types into the public API would couple the
product contract to one execution substrate and expose a much larger compatibility
surface than this phase requires.

## Decision

`AppDeploymentSpec` will express the private runtime with product-owned fields:
an immutable OCI image digest, replicas, a TCP port, CPU in millicores, memory in
MiB, and HTTP paths for liveness and readiness. Requests and limits are required,
positive, and requests cannot exceed limits. Probe paths begin with `/` and are
bounded in length. Kubernetes quantities are produced only inside the operator.

Each `AppDeployment` owns exactly one same-named Deployment and one same-named
ClusterIP Service in its namespace. The container and port are named `app` and
`http`; the Service targets that named port through a stable selector. The
operator removes public-exposure drift while preserving fields allocated by the
API server. It observes both children, recreates either child when removed, and
does not delete children directly.

The runtime uses non-root execution, `RuntimeDefault` seccomp, no privilege
escalation or capabilities, and a read-only root filesystem. Startup uses the
readiness path with a fixed 60-second window; readiness runs every five seconds
and liveness every ten seconds, both with a two-second timeout and three failures.
These values are policy in this API version rather than user configuration.

Observed release fields advance only after both children converge. `Ready=True`
requires a converged Service and a complete Deployment rollout. The operator does
not read Pods or EndpointSlices; Deployment status is the aggregate workload
signal. A conflict on either child uses the existing `OwnershipConflict` reason.

The end-to-end proof builds two versions of a local HTTP fixture with BuildKit,
loads them into a disposable Kind cluster, and references them by digest. Public
egress is a separate manual check and does not make the deterministic gate depend
on an external service.

## Consequences

The API remains independent of Kubernetes Go types while mapping deterministically
to a secure private runtime. Service identity, probe policy, resource units, and
security settings become compatibility-sensitive behavior for `v1alpha1`.

The fixed profile intentionally excludes arbitrary probe tuning, environment
variables, volumes, autoscaling, alternate security profiles, public exposure,
and direct endpoint health. Future requirements must extend the product contract
deliberately instead of exposing the underlying PodSpec.

## Alternatives Considered

Expose `corev1.ResourceRequirements`, probes, and container security types in the
CRD. This was rejected because it would make Kubernetes the public product API.

Create only a Deployment and let consumers discover Pods. This was rejected
because Pod identity is ephemeral and does not provide a stable private endpoint.

Read Pods and EndpointSlices to compute readiness. This was rejected because the
Deployment already aggregates rollout state and the additional permissions and
watches are unnecessary for this slice.

Publish the fixture image to a remote registry. This was rejected because local
BuildKit images loaded into Kind provide a reproducible proof without publication
credentials or remote mutation.

## References

- [Kubernetes Services](https://kubernetes.io/docs/concepts/services-networking/service/)
- [Kubernetes resource management](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
- [Kubernetes probes](https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/)
- [Kubernetes security context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)
