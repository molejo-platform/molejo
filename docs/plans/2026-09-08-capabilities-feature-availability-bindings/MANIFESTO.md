# Capability, binding, and feature availability foundation

## Objective

Implement a provider-neutral and cluster-scoped foundation that lets Molejo state
which application features are usable, limited, not configured, unavailable, or
unknown before the Console attempts them.

The first complete path must work without Loki, ClickHouse, Prometheus,
VictoriaMetrics, CloudWatch, OpenBao, or another optional provider:

```text
external OCI release
  -> explicit AppEnvironment placement
  -> Control Plane desired state
  -> outbound mTLS Cluster Agent
  -> Platform Operator reconciliation
  -> current Kubernetes status, logs, and events
  -> optional current CPU and memory when metrics.k8s.io is available
```

Historical telemetry, managed builds, external secret stores, storage, and public
publication remain independently composable capabilities. Their absence must not
make the core application loop unhealthy.

## Product decision

The implementation must preserve the accepted operational model:

- Foundation observes the Kubernetes substrate locally and does not mutate it.
- Platform Lifecycle installs and diagnoses Molejo-owned components.
- Capabilities are explicit operator runbooks with visible ownership, inputs,
  verification, and teardown implications.
- The Application Loop manages releases, application intent, reconciliation, and
  runtime visibility through the Control Plane and Console.
- The Cluster Agent is the authenticated authority for current cluster facts and
  bounded reads of Molejo-owned workloads.
- The Platform Operator reconciles Molejo CRs and their children; it does not
  discover, install, or communicate with third-party providers.

The product does not require every cluster to have the same optional stack. An
App is logical and portable; each AppEnvironment is placed in exactly one cluster
and receives the effective availability of that target.

## Review decision record

The Go, software architecture, Kubernetes, and Molejo vision reviews converged on
the following decisions:

1. Keep protocol capabilities, operational observations, provider bindings, and
   feature availability as separate concepts.
2. Use a small, versioned catalog and pure availability resolver; do not create a
   plugin engine or a universal provider abstraction.
3. Keep current Kubernetes data and historical provider data behind different
   interfaces and contracts.
4. Do not carry interactive logs or metrics over the existing command stream.
   Add a separate outbound mTLS query lane after the availability baseline.
5. Keep the Operator provider-neutral. Its only initial change is to publish the
   stable workload metadata contract in a shared package.
6. Keep `molejoctl foundation inspect` local and diagnostic. Durable observations
   come from the paired Agent, not from CLI uploads.
7. Add provider-specific persistence only with the first provider that needs
   runtime configuration. Do not create a generic `provider_bindings` table.
8. Prove contract parity between K3s and EKS, not identical installed components.

## Current state

### Useful foundations

- `workspace_clusters` explicitly binds a Workspace to an Agent installation.
- `app_environments.cluster_id` already makes placement cluster-specific.
- the Agent connection is outbound and authenticated with mTLS;
- runtime commands are leased, fenced, and separated from desired state;
- the Agent already reports complete snapshots for `AppDeployment` and
  `AppVolume` reconciliation;
- the Operator renders portable Kubernetes resources and stable ownership;
- external OCI Release registration is independent of source, builder, and
  registry vendor;
- Control Plane observability already has separate log, metric, and event reader
  interfaces;
- application secrets already depend on the consumer-owned `SecretValueStore`
  port;
- Console source boundaries already follow `app -> features -> shared`;
- `molejoctl` already separates Foundation, Platform Lifecycle, and Capability
  runbooks.

### Structural gaps

1. `AgentHello.capabilities` contains static protocol strings, not observed
   cluster capabilities.
2. `agent_installations.capabilities_json` persists those protocol strings and
   cannot represent support, health, reason, or freshness.
3. current logs are implemented by polling ClickHouse, while current metrics are
   implemented by querying VictoriaMetrics.
4. absence of a provider is discovered by an endpoint returning `503`, not by a
   system projection available to the Console.
5. the Agent cannot read Pods, `pods/log`, Kubernetes Events, or
   `metrics.k8s.io`.
6. the current Agent stream is sequential and is unsuitable for interactive,
   cancellable reads.
7. observability scope lacks a Cluster identity and can collide when namespaces
   and runtime names repeat in different clusters.
8. storage profiles currently map to one installation-wide StorageClass.
9. publication uses a conventional Gateway reference without an explicit
   cluster-level binding.
10. Console observability routes issue requests without first knowing whether the
    structural feature is available.
11. `installationCapabilities` and authorization capabilities already have other
    meanings and must not be repurposed.

## Contract vocabulary

### Protocol Capability

Protocol capabilities remain strings in `AgentHello` and negotiate compatible
software behavior:

```text
runtime.v1alpha1
runtime-observation.v1alpha1
capability-observation.v1alpha1
runtime-query.v1alpha1
certificate-renewal.v1alpha1
```

They do not assert that Gateway API, a StorageClass, Metrics API, logs, or a
provider is operational.

### Capability Definition

A Capability Definition is an atomic, provider-neutral guarantee known by the
product. IDs do not contain a contract version; version is a separate field.

Initial catalog:

```text
runtime.workload.apply
runtime.workload.observe
runtime.logs.current
runtime.metrics.current
runtime.events.current

telemetry.logs.historical
telemetry.metrics.historical
telemetry.events.historical

parameters.plain
parameters.secret.static

publication.http
publication.tcp
storage.rwo
storage.expand

source.github
build.managed
release.external
release.history
```

Vendor and distribution names do not become capability IDs.

### Capability Observation

A Capability Observation is an authenticated fact reported for one cluster:

```go
type Observation struct {
	ID              ID
	ContractVersion ContractVersion
	Support         Support
	Health          Health
	ProviderKind    string
	ReasonCode      ReasonCode
	Limitations     []string
	SampledAt       time.Time
}
```

Support states:

```text
Supported | Unsupported | Unknown
```

Health states:

```text
Healthy | Degraded | Unavailable | Unknown
```

Rules:

- support and health are independent;
- the Agent clock is informational, not authoritative;
- the Control Plane stores its own `receivedAt` and computes expiration;
- an expired observation resolves to `Unknown`, never `Unavailable`;
- complete snapshots replace the previous set atomically;
- partial snapshots never delete omitted observations;
- unknown but validly bounded IDs may be stored for forward compatibility, but
  an older Control Plane ignores them during feature resolution;
- observations never contain provider endpoints, credentials, Kubernetes
  manifests, raw errors, or application data.

### Provider Binding

A Provider Binding selects a concrete implementation for a capability in a
specific scope. It is a family of typed domain contracts, not one generic table.

Examples:

```text
ClusterStorageBinding
ClusterPublicationBinding
TelemetryBinding
SecretStoreBinding
GitHubInstallation
AppGitHubSource
WorkspaceCluster
AppEnvironment placement
```

The initial implementation may project adapters configured at process startup as
read-only installation bindings. A database resource is added only when an
operator needs to configure or select that binding through the product API.

Credentials remain in deployment Secrets, workload identity, IAM, or the
provider's secret mechanism. A binding exposes only a stable ID, kind, scope,
non-sensitive reference, state, reason, and observation timestamp.

### Feature Availability

Feature Availability is a derived read model:

```text
product catalog
+ target and AppEnvironment placement
+ recent Cluster Capability Observations
+ typed Provider Bindings
+ provider health evidence
+ application configuration
= effective Feature Availability
```

It is never the source of desired state and is not persisted as primary state.

Feature states:

```text
Available
Limited
NotConfigured
Unavailable
Unsupported
Unknown
```

Semantics:

- `Available`: all required guarantees are fresh and healthy;
- `Limited`: a useful subset exists and its limitations are explicit;
- `NotConfigured`: Molejo supports the feature, but an add-on, binding, or
  provider configuration is absent;
- `Unavailable`: the required configuration exists but is unhealthy;
- `Unsupported`: the selected product, Agent, or cluster version cannot satisfy
  the contract;
- `Unknown`: evidence is missing, stale, or cannot be safely interpreted.

Reason codes are stable typed constants internally and additive strings in the
OpenAPI response. Initial codes include:

```text
cluster_not_attached
cluster_agent_offline
cluster_observation_stale
cluster_capability_missing
cluster_capability_incompatible
provider_not_configured
provider_health_unknown
provider_unreachable
binding_missing
binding_degraded
metrics_api_missing
runtime_not_deployed
app_source_missing
historical_backend_missing
secret_backend_missing
```

Human copy belongs to the HTTP/Console boundary, not the functional core.

## Responsibility matrix

| Concern | Control Plane | Cluster Agent | Platform Operator | molejoctl | Console |
|---|---|---|---|---|---|
| Capability catalog | owns | consumes | none | consumes subset | generated API types |
| Cluster observation | persists and resolves | discovers and reports | none | local diagnostic only | reads projection |
| Desired application state | owns | transports/applies | reconciles | none | mutates through API |
| Current logs/metrics/events | scopes and brokers | bounded Kubernetes reads | emits normal Events only | verifies prerequisites | displays |
| Historical telemetry | provider adapters | none | none | optional runbooks | queries when available |
| Provider binding | typed domain ownership | reports cluster facts only | consumes resolved refs only | configures/verifies runbook | displays state |
| Provider credentials | references deployment identity | none unless required by current application materialization | none | accepts explicit local input | never sees |
| Cluster lifecycle | none | none | none | none | none |

## Source organization

### Shared Go contract

Create:

```text
packages/capabilitycontract/
  capability.go
  observation.go
  validation.go
  capability_test.go
  observation_test.go
```

This package contains only IDs, enums, validation, copying/normalization, and
ordering. It imports no Kubernetes, SQL, protobuf, HTTP, or Console concepts.

### Control Plane

Create:

```text
services/control-plane-api/internal/featureavailability/
  catalog.go
  model.go
  reason.go
  resolver.go
  resolver_test.go

services/control-plane-api/internal/providerbinding/
  model.go
  inventory.go

services/control-plane-api/internal/store/
  cluster_capability.go

services/control-plane-api/internal/api/
  feature_availability_handler.go
```

Later split the current observability implementation into intention-revealing
files:

```text
services/control-plane-api/internal/observability/
  model.go
  current.go
  historical.go
  clickhouse.go
  prometheus_compatible.go
  unavailable.go
```

Interfaces are declared by consumers and remain narrow. There is no interface
requiring one provider to implement every signal or temporal guarantee.

### Cluster Agent

Create:

```text
services/cluster-agent/internal/capability/
  collector.go
  kubernetes.go
  decision.go
  collector_test.go

services/cluster-agent/internal/visibility/
  target.go
  logs.go
  metrics.go
  events.go
  errors.go
  *_test.go
```

Capability discovery and interactive visibility do not belong in the runtime
mutation package.

### Platform Operator

Create or extend a provider-neutral shared metadata package:

```text
packages/kubernetes-api/metadata/
  labels.go
  annotations.go
  metadata_test.go
```

The Operator and Agent share only stable Molejo label and annotation keys. The
Operator receives no provider, feature-availability, or query dependency.

### Console

Create a vertical feature:

```text
apps/console-web/src/features/feature-availability/
  api.ts
  model.ts
  queries.ts
  FeatureAvailabilityNotice.tsx
  public.ts
  model.unit.test.ts
  feature-availability.integration.test.tsx
```

Application setup, observability, parameters, build/source, storage, and
publication consume this feature through `public.ts`.

## Implementation sequence

Each cut is independently reviewable and ends with a usable or more truthful
system. Product code must not move to the next cut while its exit criteria remain
unproven.

### Cut 0 — Freeze semantics and compatibility

Goal: make the vocabulary normative before schema or protocol changes.

Changes:

1. Add an ADR defining Protocol Capability, Capability Observation, typed
   Provider Binding, Feature Availability, scopes, states, reason codes, and
   freshness.
2. Update the operational model and capability ownership ADR only where needed
   to state that Agent observations are runtime facts, not Foundation state.
3. Record that current and historical telemetry have different guarantees.
4. Record that alpha changes are additive where inexpensive, while clean
   reinstall remains the supported alpha transition.

Verification:

- documentation uses one term for each concept;
- no provider or Kubernetes distribution appears as a product feature ID;
- non-goals and ownership agree with ADR 0015.

Exit criteria:

- reviewers can classify every planned mutation under exactly one owner.

### Cut 1 — Pure catalog and honest read model

Goal: expose useful structural availability before adding new Agent behavior.

Control Plane changes:

1. Add `packages/capabilitycontract`.
2. Add the pure `featureavailability.Resolve(now, target, facts)` functional
   core. It receives all facts and a clock value explicitly and returns a stable,
   sorted projection.
3. Add the read-only provider inventory assembled in `internal/application` from
   adapters that were successfully configured:
   - builtin external Release and Release history;
   - builtin plain parameters;
   - GitHub App configuration;
   - Build worker configuration and known health, if available;
   - OpenBao secret store configuration;
   - ClickHouse historical logs/events;
   - Prometheus-compatible historical metrics.
4. Do not mark a configured provider `Available` without health evidence. Use
   `Unknown/provider_health_unknown` until an asynchronous, domain-specific check
   has succeeded.
5. Add to OpenAPI:

```http
GET /api/v1/workspaces/{workspaceId}/feature-availability
  ?scopeType=Workspace|App|AppEnvironment
  &scopeId=...
```

6. Keep authorization and `installationCapabilities` unchanged.
7. Revalidate feature requirements in mutation handlers; the projection remains
   advisory and cannot prevent a time-of-check/time-of-use change.

Initial response behavior:

- external Release and history: `Available`;
- plain parameters: `Available`;
- deploy/status: derived from WorkspaceCluster, Agent freshness, and protocol
  capability;
- GitHub, managed builds, and secrets: based on their existing typed facts;
- current logs, metrics, and Kubernetes Events: `Unsupported` until the Agent
  query protocol exists;
- historical telemetry: `NotConfigured` or `Unknown` according to provider
  inventory;
- Control Plane operation events: `Available`.

Tests:

- table-driven matrix covering all availability states and reason precedence;
- stale facts become `Unknown`;
- Cluster A facts never satisfy an AppEnvironment in Cluster B;
- provider configured without proof is not `Available`;
- output ordering is deterministic and output slices do not alias input slices;
- real PostgreSQL HTTP integration verifies resource ancestry and Workspace
  isolation;
- response arrays are `[]`, never `null`;
- provider credentials, endpoints, and raw errors are absent.

Exit criteria:

- Console can ask one endpoint what is structurally usable without provoking
  provider failures;
- no new provider table or plugin registry exists;
- existing deploy-by-image flow remains unchanged.

### Cut 2 — Authenticated Cluster Capability Observations

Goal: replace inferred cluster availability with fresh facts from the paired
Agent.

Protocol changes:

1. Keep `AgentHello.capabilities` for protocol negotiation.
2. Add `CapabilityObservation` and `capability_snapshot_complete` to heartbeat.
3. Add `capability-observation.v1alpha1` as an optional protocol capability.
4. Bound observation count, field lengths, limitations, and timestamps.
5. Use the existing session ID and heartbeat sequence for replay and regression
   protection.

Agent changes:

1. Add a collector that runs periodically and caches a complete snapshot.
2. Detect only capabilities needed by Molejo:
   - runtime CRDs and Operator availability;
   - `pods/log` readability;
   - Events readability;
   - `metrics.k8s.io` presence and readability;
   - Gateway API resources and relevant Conditions;
   - StorageClasses and expansion characteristics.
3. Combine API discovery, `SelfSubjectAccessReview`, and a safe bounded read.
   API presence alone is not healthy capability evidence.
4. Never install or mutate the discovered component.
5. Send the cached snapshot with heartbeat without delaying runtime observation.

Persistence changes:

Add migration `030_cluster_capability_observations.sql` with a relational table:

```text
cluster_id
capability_id
contract_version
support
health
provider_kind
reason_code
sanitized_message
limitations_json
sampled_at
received_at
expires_at
observed_session_id
snapshot_sequence
```

Use `(cluster_id, capability_id, contract_version)` as the key. A complete
snapshot is reconciled in one transaction. Session mismatch or regressive
sequence never changes persisted state.

Freshness rules:

- Control Plane receipt time is authoritative;
- expiration is bounded from the heartbeat and probe intervals;
- Agent disconnect or expiration resolves features to `Unknown`;
- absence of Metrics API resolves current resource metrics to `NotConfigured`,
  without affecting deploy/status/logs;
- an authorization denial resolves the observed capability to
  `Unavailable/access_denied`.

API changes:

- optionally expose an operator-facing read-only
  `GET /api/v1/admin/clusters/{clusterId}/capability-observations`;
- never expose raw Kubernetes discovery documents or authorization review data.

Tests:

- protobuf generation and lint;
- backward-compatible Agent connects without the new capability;
- mTLS `bufconn` tests for authenticated snapshots;
- duplicate, excessive, malformed, stale, foreign-session, and regressive
  observations are rejected or safely ignored as specified;
- PostgreSQL Testcontainers proves atomic complete-snapshot replacement,
  partial-snapshot preservation, concurrency, and expiry;
- collector decision functions use table-driven tests and minimal fake clients.

Exit criteria:

- removing or restoring Metrics API changes only the corresponding observation;
- an offline Agent cannot leave a feature falsely `Available`;
- no Agent observation changes desired application state.

### Cut 3 — Console and CLI consume one vocabulary

Goal: make system availability visible without duplicating its authority.

Console changes:

1. Generate TypeScript types from OpenAPI and add the
   `feature-availability` vertical feature.
2. Fetch Workspace-level availability in Workspace composition and
   AppEnvironment-level availability in the environment layout.
3. Keep deep routes accessible. Render a durable state panel instead of removing
   the route when a feature is unavailable.
4. Do not start queries for `NotConfigured`, `Unavailable`, `Unsupported`, or
   `Unknown` features.
5. Render `Limited` with its limitations and retain the useful subset.
6. Apply the projection to:
   - external Release and deployment actions;
   - source and managed-build setup;
   - plain versus secret parameters;
   - storage and publication setup;
   - current versus historical observability.
7. Preserve existing actor permission checks. Structural availability and actor
   authorization are conjoined by the page, but their models and messages remain
   separate.
8. Provide stable action guidance, such as the relevant `molejoctl capability
   ... verify` command, only when that exact runbook exists.

Console tests:

- pure mapping from API state/reason to presentation;
- loading, stale cache, refetch, error, and each structural state;
- `enabled` query behavior prevents impossible provider calls;
- Limited current data remains visible when history is absent;
- deep links render the availability state rather than redirecting;
- permission denial and structural unavailability remain distinguishable;
- no Playwright expansion in this cut.

Molejoctl changes:

1. Add `ID`, `ContractVersion`, and stable reason codes to local capability
   observations using `packages/capabilitycontract`.
2. Expand `foundation inspect` to report the same provider-neutral IDs for local
   evidence, while remaining read-only and non-persistent.
3. Split `platform doctor` output into required Platform Lifecycle failures and
   optional capability warnings.
4. Verify that the installed Agent RBAC matches the declared observer capability.
5. Do not add observation upload, a generic provider command, or Control Plane
   administrator authentication.

Exit criteria:

- the Console no longer needs a `503` to learn that a feature is not configured;
- CLI and API use the same IDs and reason vocabulary without sharing authority;
- local inspection cannot overwrite Control Plane state.

### Cut 4 — Separate outbound runtime query lane

Goal: support bounded current application visibility without blocking durable
reconciliation.

Protocol changes:

Add a second bidirectional RPC on the existing mTLS service, opened outbound by
the Agent:

```text
OpenRuntimeQueryChannel
  RuntimeQueryHello
  RuntimeQueryRequest
  RuntimeQueryChunk
  RuntimeQueryComplete
  RuntimeQueryCancel
```

The request uses a typed `oneof` for:

```text
PodLogs
CurrentPodMetrics
KubernetesEvents
```

Required properties:

- request ID and deadline;
- logical AppEnvironment correlation;
- namespace/runtime target resolved only by the Control Plane;
- bounded filters and pagination;
- one writer goroutine per stream;
- bounded queues and explicit backpressure;
- per-cluster and global concurrency limits;
- cancellation propagated from HTTP/SSE to gRPC and Kubernetes;
- takeover invalidates the previous query stream;
- disconnection completes pending queries with a typed error;
- queries are never Operations, leases, desired state, or database work queues.

Do not accept raw Kubernetes URLs, GVRs, label selectors, field selectors,
namespaces, Pods, PromQL, LogQL, SQL, or provider-native queries from the browser.

Control Plane changes:

```text
services/control-plane-api/internal/clusteragent/
  runtime_query_broker.go
  runtime_query_channel.go
  runtime_query_broker_test.go
  runtime_query_channel_test.go
```

Agent changes:

```text
services/cluster-agent/internal/controlplane/
  query_connector.go
  query_connector_test.go
```

Tests:

- real mTLS in-memory stream;
- a slow log query does not delay heartbeat, RuntimeCommand, reconnect, or
  certificate renewal;
- cluster mismatch and stale session are rejected;
- cancellation, timeout, chunk limits, backpressure, takeover, and disconnect;
- no query state is required for post-crash reconciliation correctness.

Exit criteria:

- command reconciliation remains healthy while current logs stream;
- only the Agent session for the AppEnvironment cluster can satisfy the query.

### Cut 5 — Kubernetes current logs, events, and metrics

Goal: provide the useful baseline using resources already present in Kubernetes.

RBAC changes:

Create a separate `molejo-cluster-agent-observer` ClusterRole and binding with the
minimum required verbs:

```text
pods: get,list
pods/log: get
core events: get,list
events.k8s.io events: get,list
metrics.k8s.io pods: get,list
storage.k8s.io storageclasses: get,list
gateway.networking.k8s.io gatewayclasses,gateways: get,list
authorization.k8s.io selfsubjectaccessreviews: create
```

Do not grant:

```text
pods/exec
pods/attach
pods/portforward
nodes
nodes/proxy
nodes/log
secrets list/watch
impersonation
provider or cloud APIs
```

Target validation:

Before reading a workload, the Agent proves:

1. the namespace is Molejo-owned;
2. the requested AppDeployment is Molejo-owned;
3. Deployment or StatefulSet ownership reaches that AppDeployment;
4. Pod ownership reaches the expected workload;
5. the stable Molejo runtime label matches;
6. the requested container is an allowed application container.

RBAC cannot restrict Pods by label, so these checks prevent accidental confused
deputy behavior but do not remove the impact of a completely compromised Agent
token. Namespace-scoped credential isolation is a separate security hardening
decision.

Provider behavior:

- current logs come from `pods/log`, are bounded, ephemeral, and may include only
  current Pods and one previous container instance;
- current Events come from Kubernetes and are identified as best-effort;
- Control Plane operation events remain the durable base;
- current CPU and memory come from `metrics.k8s.io` only when available;
- readiness, desired/available replicas, rollout, and restarts come from normal
  Kubernetes object status and do not require Metrics API;
- no current endpoint silently pretends to provide historical retention.

Initial limits are constants, not user configuration:

```text
logs: 2,000 lines and 1 MiB per response
events: 200 items per request
active queries: 4 per cluster
follow duration: at most 5 minutes
```

Operator changes:

1. Move stable Molejo label/annotation keys into
   `packages/kubernetes-api/metadata`.
2. Make Operator and Agent consume the shared constants.
3. Add rendering tests proving the stable label and ownership chain.
4. Do not change CRDs, controllers, provider dependencies, or Operator RBAC for
   observability.

HTTP behavior:

- existing `/observability/logs/live` uses the Agent current-log reader;
- historical `/observability/logs` remains provider-backed;
- existing `/observability/metrics/live` uses Agent current data;
- historical `/observability/metrics` remains provider-backed;
- Events response adds source, partial status, and unavailable sources;
- current and historical cursors remain distinct opaque contracts.

Tests:

- fake clients for deterministic Pod selection, aggregation, truncation, and
  sanitization;
- foreign namespace/runtime/Pod/container is denied;
- absence of Metrics API affects only current CPU/memory;
- contract tests prove allowed and forbidden RBAC rules;
- envtest proves labels, ownership, and conditions but is not treated as proof of
  kubelet logs or Metrics API;
- Kind proves real `pods/log`, Events, Agent ServiceAccount RBAC, cancellation,
  and foreign workload denial.

Exit criteria:

- a K3s installation without historical providers shows current logs and Events;
- current resource metrics are conditional on Metrics API;
- historical endpoints remain explicitly `NotConfigured` when unbound;
- the Operator remains unaware of telemetry providers.

### Cut 6 — Separate current and historical provider ports

Goal: make existing adapters honest extensions rather than baseline dependencies.

Control Plane changes:

1. Split the existing aggregate Reader into consumer-owned ports:

```text
CurrentLogReader
CurrentMetricReader
CurrentEventReader
HistoricalLogReader
HistoricalMetricReader
HistoricalEventReader
```

2. Route current ports to the Agent query broker.
3. Route historical ports only to configured providers.
4. Rename `VictoriaMetricsClient` to a Prometheus-compatible adapter because its
   actual contract is `/api/v1/query` and `/api/v1/query_range`.
5. Keep ClickHouse as an OTel-schema-specific historical adapter.
6. Add Cluster ID, Cluster UID, Workspace ID, AppEnvironment ID, namespace, and
   runtime name to the internal observability scope.
7. Require historical bindings to prove the Molejo identity/label schema; an
   HTTP-compatible query endpoint alone is insufficient.
8. Do not expose PromQL, LogQL, SQL, CloudWatch query syntax, or provider URLs in
   public APIs.

Provider inventory behavior:

- adapter absent: `NotConfigured`;
- adapter configured without health evidence: `Unknown`;
- adapter healthy and schema verified: `Available`;
- adapter configured but unreachable: `Unavailable`;
- partial signal support: `Limited`.

Events behavior:

- Control Plane operations are always available;
- Kubernetes Events are appended when current queries are healthy;
- historical provider Events are appended when bound;
- response identifies sources, partial results, and unavailable sources.

Exit criteria:

- no provider is required to implement unrelated signals;
- no historical endpoint falls back to current Kubernetes data;
- two clusters with repeated namespace/runtime names cannot share observations or
  queries accidentally.

### Cut 7 — Typed Kubernetes bindings

Goal: remove the two known single-cluster assumptions without creating a generic
provider model.

Storage:

1. Introduce `ClusterStorageBinding` between the portable StorageProfile and one
   cluster StorageClass.
2. Record cluster ID, StorageClass name, provisioner, access modes, expansion,
   binding mode, observed health, and freshness.
3. Resolve the runtime StorageClass using `AppEnvironment.cluster_id` before
   producing `AppVolume` desired state.
4. Keep storage optional for stateless applications.
5. Prove a binding with a PVC plus consuming Pod smoke; StorageClass presence
   alone is not readiness.

Publication:

1. Represent the current shared Gateway/listener convention as a
   `ClusterPublicationBinding`.
2. Observe GatewayClass acceptance, Gateway programming, listener state, and
   supported route kinds.
3. Keep the existing conventional reference for the first K3s slice.
4. Change the Operator contract only when a second concrete Gateway reference is
   required; pass a resolved, typed reference rather than a provider type.
5. The Operator owns HTTPRoute/TCPRoute children, never GatewayClass, Gateway,
   certificates, DNS, or load balancers.

Molejoctl:

- existing storage/publication runbooks may create or verify infrastructure only
  after explicit operator confirmation;
- the Agent observes resulting cluster facts;
- registering a binding in the Control Plane is added only with a typed API and
  an authenticated operator workflow;
- runbook YAML and credentials never become Control Plane state.

Registry:

- do not add a universal registry capability in this cut;
- image pull remains a runtime outcome because EKS node identity, a pull Secret,
  and a public registry have different configuration paths;
- surface `ImagePullBackOff` and related Events as precise deployment diagnosis;
- retain `molejoctl capability registry` as an explicit cluster runbook.

Exit criteria:

- the same App can have AppEnvironments on K3s and EKS without reusing the wrong
  StorageClass or Gateway reference;
- a stateless private deployment remains independent of storage/publication;
- no cloud provider name enters AppDeployment or AppVolume product intent.

### Cut 8 — Conformance, packaging, and rollout

Goal: prove the contract from deterministic local tests through real K3s and EKS
without making real clusters part of the default fast suite.

Test ladder:

1. pure table-driven tests for catalog, resolver, freshness, probe decisions,
   target ownership, state precedence, and limits;
2. real PostgreSQL through the existing Testcontainers path for migration,
   snapshot reconciliation, expiry, and API integration;
3. real mTLS and in-memory gRPC for observations and runtime queries;
4. envtest for CRDs, ownership, conditions, storage/gateway observations, and
   Operator rendering;
5. one small Kind acceptance for Agent RBAC, Pod logs, Events, runtime lifecycle,
   cancellation, and negative authorization;
6. manual K3s acceptance;
7. manual EKS acceptance after K3s is stable.

Add one delegated repository target and keep orchestration in a script:

```text
tools/testing/kubernetes-conformance.sh

profiles:
  core
  metrics-current
  storage-rwo
  publication-http
```

The Justfile receives at most one target that delegates to the script. The fast
default suite does not provision EKS or install optional historical stacks.

Core conformance for both K3s and EKS:

1. install/upgrade the same Molejo chart artifacts;
2. pair the Agent through outbound mTLS;
3. register an immutable external OCI Release;
4. create an AppEnvironment with explicit Cluster ID;
5. reconcile a private stateless application;
6. observe readiness and rollout state;
7. retrieve current logs and Kubernetes Events;
8. retrieve CPU/memory only when Metrics API is observed;
9. prove historical telemetry reports `NotConfigured` without affecting runtime;
10. delete the application and prove idempotent cleanup.

Optional profiles then prove storage and publication independently.

Deployment order:

1. apply the additive PostgreSQL migration and deploy the backward-compatible
   Control Plane;
2. deploy the Agent with optional observation protocol and existing runtime
   behavior intact;
3. verify observation freshness and availability API;
4. deploy the Console consumer;
5. deploy the runtime query lane and observer RBAC;
6. verify current diagnostics;
7. enable typed storage/publication bindings only after their profile smoke
   succeeds.

Rollback behavior:

- an old Agent continues to connect and new operational facts resolve to
  `Unknown`;
- unknown protobuf fields are ignored by old binaries;
- losing the query lane affects only current diagnostics;
- additive observation tables may remain through a binary rollback;
- optional provider failure never makes the Control Plane `/readyz` fail;
- no rollback changes desired application state from availability observations.

Repository gates:

```text
just generate
just test
just integration-test
just frontend-check
just frontend-build
just verify
git diff --check
```

Exit criteria:

- the same core contract passes Kind, K3s, and EKS without distribution branches
  in product domain code;
- optional capability differences appear as explicit states and reasons;
- chart and release artifacts contain the new Agent observer RBAC and generated
  contracts;
- onboarding documentation points to one conformance path.

## Provider expansion after the baseline

The following are future vertical slices, not work bundled into the baseline:

### Historical metrics

- bind a Prometheus-compatible query provider;
- separately prove collection and the Molejo label schema;
- allow Prometheus, VictoriaMetrics, or a managed compatible service to satisfy
  the same product capability;
- keep current resource metrics independent.

### Historical logs

- add exactly one typed `TelemetryBinding` with the first real provider;
- normalize time range, cursor, limit, text, severity, and Molejo resource
  identity;
- allow Loki-compatible, ClickHouse/OTel, or CloudWatch Logs adapters to evolve
  independently;
- advertise provider-specific search extensions separately from the core query.

### Secret stores

- keep `parameters.plain` builtin;
- select one static-secret profile before adding another adapter;
- validate SSM semantics rather than claiming equivalence with OpenBao KV v2;
- model static, versioned, rotation, and dynamic-secret guarantees separately;
- never fall back to plaintext PostgreSQL or treat Kubernetes Secret as the
  durable vault.

### Managed builds and sources

- keep external Release registration builtin and independent;
- source and managed build remain separate features;
- report GitHub App, Workspace installation, App source, builder, and worker
  health as separate facts;
- a configured builder without worker health evidence resolves to `Unknown`, not
  `Available`.

## Explicit non-goals

- generic plugin engine, dynamic module loading, or provider marketplace;
- universal `providers(config_json)` or `provider_bindings(config_json)` tables;
- automatic installation of Loki, ClickHouse, Prometheus, VictoriaMetrics,
  OpenBao, cert-manager, Gateway controllers, CSI drivers, or cloud add-ons;
- implementing Loki, CloudWatch Logs, SSM, AWS Secrets Manager, or another new
  provider in this baseline;
- cluster provisioning, upgrades, nodes, taints, CNI, IAM, DNS, load balancers,
  or cloud control-plane administration;
- multi-cluster scheduler, automatic placement, failover, or workload migration;
- requiring identical optional capabilities in all clusters;
- proxying arbitrary Kubernetes APIs through the Agent;
- exposing provider-native query languages in the public API;
- putting feature availability, telemetry, or provider selection in Molejo CRDs;
- changing actor authorization or permissions;
- making optional provider health part of Control Plane `/readyz`;
- broad Playwright coverage;
- release publication or production deployment as part of implementing an
  individual cut.

## Global acceptance criteria

1. Every AppEnvironment availability decision is scoped by its stored Cluster ID.
2. Protocol capability, capability observation, provider binding, feature
   availability, and actor authorization remain distinguishable in code and API.
3. Agent disconnection or observation expiry produces `Unknown`, not a stale
   `Available` result.
4. The Control Plane remains the only source of desired application state.
5. Current diagnostics use only Molejo-owned workloads and cannot address
   arbitrary Kubernetes resources.
6. The runtime query lane cannot delay heartbeat, reconciliation commands, or
   certificate rotation.
7. Historical endpoints never silently fall back to ephemeral Kubernetes data.
8. Optional provider absence does not prevent external OCI deploy and runtime
   status.
9. The Platform Operator contains no third-party provider dependency or feature
   availability logic.
10. The Console explains structural state before attempting an impossible call
    and preserves useful Limited behavior.
11. `molejoctl` runbooks remain explicit and local; Agent observations remain the
    durable runtime authority.
12. No credentials, provider endpoints, raw authorization review, certificates,
    tokens, or secret values appear in observations, bindings, API responses,
    logs, metrics, or test snapshots.
13. K3s and EKS pass the same core conformance contract while legitimately
    reporting different optional capabilities.
14. Generated protobuf, OpenAPI, sqlc, CRDs, RBAC, and frontend types are
    reproducible and clean.

## Recommended delivery boundaries

Implement and commit each cut independently:

1. `docs(architecture): define capability availability contracts`
2. `feat(control-plane): expose structural feature availability`
3. `feat(agent): report cluster capability observations`
4. `feat(console): reflect structural feature availability`
5. `feat(agent): add bounded runtime query channel`
6. `feat(platform): expose current Kubernetes diagnostics`
7. `feat(platform): bind storage and publication per cluster`
8. `test(platform): add Kubernetes conformance profiles`

Each commit that changes more than three files must include a body describing the
contract, implementation boundary, and verification performed.
