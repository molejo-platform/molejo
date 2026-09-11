# HTTP publication foundation

The executable foundation supports one HTTP endpoint per AppEnvironment, one
named backend port, and up to ten exact addresses. An endpoint with no addresses
is omitted. Each address produces one owned HTTPRoute; every route targets the
same Service and workload. TCP publication and storage retain their existing
contracts. This is the phase-one runtime foundation: administrative persistence,
product APIs, CLI connection workflows, and Console consumption follow in later
phases. The old product HTTP configuration cannot be dispatched to this runtime.

## Ownership and contracts

The Control Plane's pure `domain.PublicationDomain` describes either an `Exact`
name or a `SubdomainPool`. `PublicationGrant` connects that administrative domain,
Workspace and binding. Exact names reject a label. Pools issue one label and
exclude reserved names and their subtrees, including the root. Normalization
trims whitespace and a final dot, lowercases ASCII, validates DNS lengths and
A-labels, and rejects URLs, paths, ports, addresses and raw Unicode. No DNS lookup
is needed: an authorized name does not imply public reachability or a DNS zone.

The concrete `kubernetesbinding.HTTPBinding` owns Gateway coordinates and a
bounded list of named listeners. Creation receives a new ID; edits increment
`Revision`; delete/recreate must receive a new ID. `SchemaVersion` identifies the
implementation contract and is independent of revision and observed Gateway UID.

The integration selects among granted destinations. An explicit compatible
listener choice is preserved. Without a choice, an exact match precedes a
wildcard; equivalent candidates require selection. A wildcard never grants its
apex. Binding configuration and matching do not themselves authorize a developer.

Each `runtimecontract.HTTPAddress` includes an immutable `HTTPDestination`:

```json
{
  "name": "web",
  "type": "HTTP",
  "portName": "http",
  "addresses": [{
    "hostname": "example.test",
    "destination": {
      "bindingId": "binding-one",
      "bindingRevision": 3,
      "schemaVersion": "kubernetes-http.v1alpha1",
      "gatewayNamespace": "edge",
      "gatewayName": "shared",
      "sectionName": "apex"
    }
  }]
}
```

A retry uses the saved destination. `SupportsSnapshot` permits additive binding
edits while rejecting a different creation, Gateway, removed listener, or
incompatible hostname. Current authorization must be rechecked independently.
The Operator does not resolve product grants or invent a replacement listener.

Typed implementation configuration may eventually use discriminator + versioned
JSONB when it forms a cohesive atomic value. This is not a platform-wide storage
convention. IDs, revisions, audit, grants, claims and dependencies remain explicit
relationships. Phase two chooses columns versus JSONB from actual transaction and
query requirements; raw provider JSON never becomes unrestricted domain state.

## Runtime and evidence

`runtime.v1alpha2` is required on both sides of the Agent channel. The executor
rejects older/missing schema identifiers, unknown fields, trailing JSON and an
invalid allocation before executing. The Kubernetes adapter dry-runs the CRD
projection with strict field validation before creating configuration objects.
This alpha cut requires matching CP, Agent, Operator and CRDs from a clean install;
there is no exposure/slug reader or converter in the runtime publication path.
An old Operator is not a supported peer even if it can connect to Kubernetes.

Route names hash AppDeployment identity, endpoint name and normalized hostname.
List order is not identity. Controller ownership uses the AppDeployment UID;
foreign routes are never adopted. Removal reads owned routes and uses UID and
resourceVersion preconditions, preserving other addresses. Deleting an
AppDeployment uses foreground propagation so an acknowledgement cannot precede
removal of its dependent routes. A route with a blocking finalizer remains pending.

Each address reports its binding snapshot, Gateway UID, route UID/generation and
conditions correlated with AppDeployment generation. Route parent conditions must
match the selected parent and current route generation; ambiguous parent reports
are not accepted. A Gateway watch selects all applications using that Gateway,
not a hard-coded installation target. A degraded address cannot be masked by a
healthy address or a progressing workload.

`RouteReady` and `GatewayReady` are separate facts. `ConnectivityVerified` and
`ServedTLSVerified` remain `Unknown/NotInspected` in runtime observations. The
read-only inspector keeps successful facts if a different read is denied. Denied
Secret access never means an invalid certificate. The Operator never reads keys.
HTTPRoute does not prove plaintext or encryption between Gateway and backend.
Certificate custody, renewal and DNS remain with their configured owners.

## Ordering and the phase-two transaction boundary

The existing CP worker serializes commands under leases and fences results;
Agent sessions reject obsolete sequences and commands have lease-bounded
deadlines. The Agent additionally rejects a desired version older than a live
AppDeployment and uses resourceVersion on updates. These checks do **not** make
absence a durable fence: after deletion, a delayed create can find no object.

Before enabling product HTTP publication, phase two must atomically persist the
resolved operation snapshot and executable pending claims. The CP owns a durable
withdrawal barrier in the existing operation/claim lifecycle: stop dispatching
older applies, account for every issued command's deadline and session, wait for
in-flight work to finish, then perform and observe foreground withdrawal. If
execution completion cannot be established, keep withdrawal pending and the claim
reserved. A timeout or an old ACK alone cannot release it. Reconnect must not
redispatch a superseded apply. The final correlated absence must be obtained
**after** quiescence; earlier absence is insufficient.

The required transaction test is Apply → lost lease/disconnection → requested
Delete → late Apply/ACK → quiescence → final removal → claim release. Until that
is proven, the API cannot issue new HTTP publication commands. This design reuses
operations and claims rather than adding a second local tombstone engine. It
requires persistence work in phase two even on a freshly reset cluster.

## Integrated consumer examples

| Scenario | Contract outcome and owner |
| --- | --- |
| Exact domain plus pool | Administrator grants both; developer supplies no label for exact and one label for pool, chooses an allowed target and named port. Server returns canonical names. |
| Saved revision differs from applied | Configuration saved; existing deployment operation remains separate. Console and CLI display both revisions. |
| Removal while Agent is offline | Keep applied address and claim, show withdrawal pending; no inference of absence or automatic reassignment. |
| Concurrent edit | Revision conflict (`409`) preserves the draft; user reloads explicitly. |
| Reserved or occupied name | Reject the permitted field/name without revealing another Workspace's occupant. |
| Listener ambiguity | Require a choice from authorized candidates, never select by list order. |
| Binding recreated | New ID rejects previous evidence and snapshots even if revision and coordinates repeat. |

These are consumer requirements for phase two onward, not implemented HTTP routes
or simulated end-to-end acceptance. The existing session contract has a real
HTTPS/cookie-jar/MFA/Origin/CSRF/revocation integration test for the future ephemeral
CLI client; no new authentication mechanism or persistent login is introduced.

## Validation

- `just operator-test`: CRD rejection, two addresses/listeners, deterministic
  routing, idempotence, ownership, independent removal and correlated status.
- `just cluster-agent-test`: strict command boundary, projection before effects,
  transport correlation and current-generation binding observation.
- Shared package/domain tests: exact/pool reservations, limits, selection,
  snapshot compatibility and read-only inspection under denied access.
- `just integration-test`: existing PostgreSQL behavior and real HTTPS session
  contract. Does not claim the future multi-address claim transaction is implemented.
- `tools/testing/publication-kind.sh`: isolated disposable Kind, real Traefik,
  verified Host/SNI and certificate, same application, partial removal and negative
  Gateway/Secret RBAC. Its TLS trust is local test trust, not public Internet trust.

Remote K3s, DNS and edge acceptance require separately authorized deployment.
