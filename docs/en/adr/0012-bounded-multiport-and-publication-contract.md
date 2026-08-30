# ADR-0012: Bounded Multiport and Publication Contract

Status: Draft

## Context

Applications may listen on more than one port and some protocols cannot use an
HTTP reverse proxy. Exposing Kubernetes Services, NodePorts, Gateway listeners,
or provider load balancers as product configuration would couple users to an
installation and would make allocation races part of the public API.

## Decision

Each AppEnvironment configuration declares one to eight named TCP container
ports. Startup, readiness, and liveness probes reference a port by name and may
use HTTP or TCP. Publication is a separate bounded list with at most one HTTP
endpoint and one experimental TCP endpoint.

The control plane, not the client, allocates TCP external ports. PostgreSQL owns
transactional publication claims for hostnames and external ports and records
the desired and current configuration versions. Deploying a historical
configuration reserves its claims again before creating the immutable Deployment
snapshot. The API never accepts a client-selected external port.

The Operator projects the intent into one ClusterIP Service. HTTP publication
uses HTTPRoute and TCP publication uses TCPRoute against named listeners on the
shared Gateway. Per-endpoint status is reported independently. The installation
advertises an exact TCP capability and privately provides the bounded listener,
NodePort, proxy, and firewall pool.

## Consequences

The product contract remains portable across Kubernetes providers while the lab
can expose PostgreSQL, Redis, RabbitMQ, and similar TCP services. Capacity is
explicit and fail-closed: exhaustion or a conflicting claim returns a stable
conflict instead of stealing an address.

The first increment supports TCP only, automatic allocation, one HTTP and one TCP
publication per AppEnvironment, and a 16-port lab pool. It does not support UDP,
TLS or SNI routing, arbitrary external ports, multiple TCP publications, custom
domains, shared claims, or production availability guarantees.

## References

- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Gateway API TCPRoute](https://gateway-api.sigs.k8s.io/api-types/tcproute/)
