# HTTP publication

The executable foundation supports one HTTP endpoint per AppEnvironment, one
named backend port, and up to ten exact addresses. A private configuration omits the HTTP endpoint. Each address produces one owned HTTPRoute; every route targets the
same Service and workload. TCP publication and storage retain their existing
contracts. Phase two provides administrative persistence, product APIs, immutable
execution snapshots and protected withdrawal. CLI administration is phase three;
Console consumption is phase four. This alpha cut has no HTTP compatibility reader,
converter, dual contract or data backfill. Migration 036 requires a clean alpha
installation; its downgrade is explicitly rejected. Reset instead of rolling back
to the former HTTP schema.

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

The binding is stored as `kind=KubernetesHTTP` with a closed, versioned JSONB
configuration. Identity, administrative revision, cluster relationship, grants,
claims and audit remain columns and relationships. Domains have no Kubernetes
coordinates. One binding per cluster and global hostname uniqueness are explicit
limits of this release, including names resolved by private DNS.

## Administrative and developer API

All administrative operations require `ManageBindings`; writes use the existing
session, MFA when enrolled, Origin and CSRF checks. App automation tokens do not
gain installation permissions. Reads do not inspect or mutate Kubernetes.

| Surface | Contract |
| --- | --- |
| `/api/v1/admin/publication/domains` | GET a bounded administrative list. |
| `/api/v1/admin/publication/domains/{domainId}` | PUT create using a caller-selected stable ID; GET recovery; PUT/DELETE use `If-Match` for an existing revision. |
| `/api/v1/admin/publication/domains/{domainId}/grants` | GET granted Workspace/binding relationships. |
| `/api/v1/admin/publication/domains/{domainId}/grants/{workspaceId}/{bindingId}` | Idempotent PUT/DELETE of the explicit relationship. |
| `/api/v1/admin/clusters/{clusterId}/bindings/publication/http` | Existing GET/PUT/DELETE surface, now with `schemaVersion`, Gateway and `listeners`; creation allocates a new binding ID. |
| `/api/v1/admin/publication/dependents?domainId=…&bindingId=…` | GET Desired, Applied and Executable references; at least one filter is required. |
| `/api/v1/workspaces/{workspaceId}/publication-options?clusterId=…` | GET choices granted to this Workspace and placement, including unknown/degraded choices. |

New lists return `{items, hasMore}`, at most 100 items, and accept `offset` from
0 to 1,000,000. Continue with `offset + 100`; concurrent administrative edits may
change list membership, so mutation safety always uses transactional checks.
Existing hierarchy routes and authorization remain authoritative for configuration,
deploy, status and withdrawal. The session's static publication domains now describe
TCP only; HTTP authorization comes from the contextual catalogue.

Example exact domain input: `{"name":"molejo.dev","kind":"Exact","reservedNames":[]}`.
Example pool input: `{"name":"molejo.dev","kind":"SubdomainPool","reservedNames":["admin.molejo.dev"]}`.
A binding for both contains listeners such as
`[{"name":"apex","hostname":"molejo.dev"},{"name":"apps","hostname":"*.molejo.dev"}]`.
The Gateway/listeners must be operated separately; saving this declaration does
not install them, issue a certificate or edit DNS.

The developer configuration uses the existing `publicEndpoints` list:

```json
{
  "name": "web",
  "type": "HTTP",
  "portName": "http",
  "addresses": [
    {"domainId":"home","bindingId":"pbd-example"},
    {"domainId":"apps","bindingId":"pbd-example","label":"welcome","listenerName":"apps"}
  ]
}
```

The server returns canonical `hostname` and resolved `listenerName` for each
association. Echoed hostnames never authorize or redirect publication. HTTP rejects
the former `domainId`/`hostnameLabel` fields on the endpoint. These fields remain
specific to the unchanged TCP contract. Omitting HTTP entirely requests a private
configuration; an HTTP endpoint with an empty addresses list is invalid.

Domain creation can be retried at the same ID with the same canonical value;
a different value conflicts. For edits and binding creation response loss, GET the
current revision before deciding whether to retry. Grant writes are set operations.
Deploy and withdrawal reuse existing idempotency keys: a key with a different
request conflicts. Saving the same resolved configuration does not advance its
revision and never schedules a deployment.

`publication_not_granted`, `publication_reserved`, `publication_limit_exceeded`,
`publication_mode_unsupported`, `publication_listener_required` and
`publication_destination_unavailable` distinguish corrective actions. Version,
occupation and dependency conflicts return 409. Occupation errors do not reveal
another Workspace or application. Administrative revocation and destructive edits
fail while protected references exist; deleting a grant precedes deleting its
domain or binding.
## Runtime and evidence

`runtime.v1alpha3` is required on both sides of the Agent channel. The executor
rejects older/missing schema identifiers, unknown fields, trailing JSON and an
invalid allocation before executing. The Kubernetes adapter dry-runs the CRD
projection with strict field validation before creating configuration objects.
This alpha cut requires matching CP, Agent, Operator and CRDs from a clean install;
there is no exposure/slug reader or converter in the runtime publication path.
An old Operator is not a supported peer even if it can connect to Kubernetes.

Route names hash AppDeployment identity, endpoint name and normalized hostname.
List order is not identity. Controller ownership uses the AppDeployment UID;
foreign routes are never adopted. Removal reads owned routes and uses UID and
resourceVersion preconditions, preserving other addresses. Withdrawal sets terminal `spec.withdrawn=true` on the existing AppDeployment.
The Operator removes its owned routes, workloads and Services using foreground
propagation and UID/resourceVersion preconditions. A blocking finalizer keeps
withdrawal pending. The root remains as a write barrier; its workload/configuration
is not executed. CRD transition validation rejects true-to-false withdrawal.

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

## Ordering, reservations and withdrawal

The database serializes HTTP allocation and administration with one transaction
advisory lock. A configuration revision and all desired claims commit together;
a conflict in one name rolls back the entire set. Deploy adds separate executable
references and an immutable destination snapshot without overwriting desired
references. Retries and drift repair read that snapshot and recheck the grant and
binding compatibility, never resolve a replacement destination from current policy.
A compatible additive binding edit does not invalidate its saved destination.

The CP records an attempt before dispatch: operation, fencing token, Agent session,
deadline and state. `Issued` can become `Completed` or `Uncertain`; confirmation of
a newer projection or withdrawal can mark older uncertain work `Fenced`.
A timeout, lost response or late ACK alone never releases reservations. A newer
success retires previous execution references and advances applied claims atomically.
Saving a different desired configuration keeps executable and applied claims alive.

Withdrawal has an explicit FSM: `None → Requested → Removing → Confirmed`.
The request supersedes pending/running Apply operations while retaining their claims.
`Removing` means the withdrawal command has been issued, not that removal succeeded.
The Agent sets or creates the terminal root; resourceVersion rejects delayed updates,
retained identity rejects delayed creates, and CEL prohibits reopening. The Operator
confirms `Withdrawn=True` only after owned children are gone, for the current root
generation. The Agent result includes the root UID and withdrawal confirmation, persisted on
the exact execution attempt; only
a matching current operation lease can archive the AppEnvironment and release claims.

An offline Agent leaves withdrawal pending. Reconnect cannot redispatch superseded
Apply operations. Terminal roots are excluded from live Agent inventory. The retained root must not
be manually deleted while old executions
could still write; it is part of the runtime safety contract, not a workload. Normal
Operator reconciliation is serialized by object key. External actors bypassing that
contract, manual namespace/root removal, or mixed-version components are unsupported.

Per-address observations retain root UID/generation, operation desired version,
exact destination and route/Gateway evidence. The CP correlates them with a known
snapshot and rejects regression by collection time and generation. Binding evidence
is scoped to its incarnation/revision and listener; missing samples retain prior
facts until their original 90-second TTL expires. A recent sibling sample does not
refresh another listener. Administrative edits clear obsolete observation data.
`publicationObservation.state` aggregates route evidence; expired observations become
Unknown on read. It never promotes connectivity or TLS verification from route status.

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

These API journeys are covered by PostgreSQL integration tests. CLI and Console
consumption remain subsequent phases. The existing session contract has a real
HTTPS/cookie-jar/MFA/Origin/CSRF/revocation integration test for the future ephemeral
CLI client; no new authentication mechanism or persistent login is introduced.

## Validation

- `just operator-test`: CRD rejection, two addresses/listeners, deterministic
  routing, idempotence, ownership, independent removal and correlated status.
- `just cluster-agent-test`: strict command boundary, projection before effects,
  transport correlation and current-generation binding observation.
- Shared package/domain tests: exact/pool reservations, limits, selection,
  snapshot compatibility and read-only inspection under denied access.
- `just integration-test`: PostgreSQL transactions, overlapping allocations,
  grant/revocation races, durable execution claims, supersession/late ACK, binding
  recreation, stale observations and the integrated HTTP API journey. Also checks
  the real HTTPS session/MFA contract.
- `tools/testing/publication-kind.sh`: isolated disposable Kind, real Traefik,
  verified Host/SNI and certificate, same application, partial removal and negative
  Gateway/Secret RBAC. Its TLS trust is local test trust, not public Internet trust.

Remote K3s, DNS and edge acceptance require separately authorized deployment.
