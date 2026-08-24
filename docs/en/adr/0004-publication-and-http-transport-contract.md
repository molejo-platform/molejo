# ADR-0004: Publication and HTTP Transport Contract

## Status

Draft

## Context

The private stateless backend provides a stable ClusterIP Service but does not
offer an optional public endpoint. The next vertical slice must publish the same
backend through shared platform infrastructure without making DNS, certificates,
Gateway implementation details, or Kubernetes authorization part of the product
contract. It must also preserve long-lived HTTP transports and expose a
deterministic status when workload and publication states change independently.

Hostname uniqueness cannot be expressed as a local CRD schema invariant because
it depends on other objects. The current repository also has no product API or
database in which to enforce a transactional uniqueness constraint.

## Decision

`AppDeploymentSpec` uses the closed enum `Private | Public`. `Private` is the
default and requires the slug to be absent. `Public` requires one lowercase DNS
label in `spec.slug`; the resulting hostname is
`{slug}.molejo.dev`.

Every workload continues to own one same-named Deployment and ClusterIP Service.
A public workload additionally owns one same-named HTTPRoute in its namespace.
The route attaches to the `https-molejo` listener of the shared `fruto` Gateway in
`fruto-system` and forwards to the workload Service. Returning to `Private`
deletes only the owned HTTPRoute. The operator does not create Gateways, DNS
records, or certificates.

Until the product control plane exists, the operator provides deterministic,
eventually consistent hostname ownership. An established owned route is retained;
otherwise creation timestamp, namespaced name, and UID break ties. If duplicate
routes already exist, the losing AppDeployment removes only its own route and
reports `HostnameConflict`. It preserves observed release fields and retries
after five minutes. A future control plane must replace this allocation boundary
with an atomic uniqueness constraint while keeping the external behavior.

Public readiness requires a complete Deployment rollout, current HTTPRoute
conditions from the expected parent (`Accepted=True` and `ResolvedRefs=True`),
and a current shared Gateway. The Gateway must report `Programmed=True`, and its
single `https-molejo` listener must report `Accepted=True`, `Programmed=True`, and
`ResolvedRefs=True`. Multiple controllers reporting the same effective route
parent are ambiguous and keep the route progressing. Missing or stale
Gateway/listener conditions use `GatewayProgressing` while the Gateway converges.
An absent Gateway, a current rejected Gateway/listener, or the absence of one
unique `https-molejo` listener after the Gateway reports `Programmed=True` uses
`GatewayRejected`. A known workload failure takes precedence over publication
progress. Route or Gateway failures produce sanitized public conditions without
exposing technical errors.

The deterministic end-to-end test installs Gateway API and Traefik in a disposable
Kind cluster. It uses an ephemeral wildcard certificate trusted by the test
client and proves REST, GraphQL, incremental SSE, persistent WebSocket, route
removal, and continued private Service access. This is local transport evidence,
not proof of public DNS or a publicly trusted certificate. External foundation
acceptance remains separate.

## Consequences

Private and public workloads share one runtime and Service identity. Publication
is reversible and does not require host ports, direct Pod access, or DNS mutation
by the operator. REST, GraphQL, SSE, and WebSocket need no protocol-specific route
objects because they use the same HTTPRoute and backend port.

Hostname allocation is safe by convergence for the current single-replica,
pre-alpha operator, but it performs a cluster-wide AppDeployment lookup and is not
a transactional product-level reservation. Higher controller concurrency,
multiple replicas, and large-scale allocation require a dedicated indexed or
control-plane claim mechanism.

Gateway, listener, domain suffix, and retry policy are compatibility-sensitive
platform policy in `v1alpha1`. Custom domains, authentication, traffic splitting,
and certificate lifecycle remain outside this decision.

## Alternatives Considered

Create one Ingress per public workload. This was rejected because Gateway API
provides an explicit shared Gateway boundary and structured route status.

Expose the Service as `LoadBalancer` or `NodePort`. This was rejected because it
would bypass the shared HTTPS policy and allocate infrastructure per workload.

Allow users to supply arbitrary hostnames or HTTPRoute fields. This was rejected
because it would expose Kubernetes infrastructure policy through the product API
and expand the compatibility surface prematurely.

Require transactional global uniqueness inside the operator. This was rejected
for this phase because Kubernetes list-and-create operations cannot provide that
product-level transaction. The operator instead converges duplicate projections,
while the future control plane owns atomic allocation.

Treat successful local TLS as proof of public availability. This was rejected
because port-forwarded Kind traffic does not validate public DNS, external
routing, or a publicly trusted production certificate.

## References

- [Gateway API HTTPRoute](https://gateway-api.sigs.k8s.io/api-types/httproute/)
- [Gateway API status](https://gateway-api.sigs.k8s.io/guides/status/)
- [Kubernetes owner references](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
- [ADR-0002: Reconciliation State and Observability Contract](0002-reconciliation-state-and-observability-contract.md)
- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
