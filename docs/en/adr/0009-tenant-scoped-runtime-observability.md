# ADR-0009: Tenant-Scoped Runtime Observability

Status: Draft

## Context

Users need runtime logs, basic metrics, and operational events without access to
Kubernetes or storage backends. Queries cross Workspace, Project, App, and
AppEnvironment boundaries, while telemetry backends are operational components
whose availability and topology may change independently from managed workloads.

## Decision

The public API resolves the complete product hierarchy and injects the trusted
Namespace and runtime identity into every query. Clients cannot provide tenant
selectors. Historical logs are limited to 24 hours, events to 7 days, and
metrics to 30 days. The API chooses a resolution of at most 1,000 points per
metric series; live logs use authenticated SSE with per-actor concurrency and
duration limits.
Current metric snapshots use a separate authenticated SSE budget and a polling
interval aligned with collection. Both streams send heartbeats, expire, and
revalidate authorization while connected. Concurrent metric streams for the
same AppEnvironment share a short-lived snapshot instead of multiplying backend
queries. Partial metric responses identify unavailable signals explicitly.

Every stored log receives a stable ingestion identifier and timestamp. Historical
navigation uses opaque keyset cursors over a fixed snapshot; live SSE sends bounded
batches whose event ID is an opaque resumable cursor. Reconnection through
`Last-Event-ID` is therefore idempotent and does not depend on matching message
contents or on a single polling window.

OpenTelemetry Collectors form the portable ingestion boundary. A node agent
collects container logs and kubelet metrics, while a cluster collector gathers
Kubernetes events. A gateway enriches and exports signals. The laboratory
deployment uses ClickHouse for short-lived logs/events and VictoriaMetrics for
metrics, behind NetworkPolicies and internal Services. These are replaceable
deployment adapters, not public product contracts. Application availability and
control-plane readiness do not depend on telemetry storage availability.
The node agent drops signals that do not carry the managed AppEnvironment label,
and the cluster collector keeps only the managed runtime Namespace. Collector
health metrics are exported through a separate internal pipeline. ClickHouse
promotes the trusted Namespace and runtime scope to typed materialized columns
with skipping indexes, while VictoriaMetrics enforces query time, series, point,
and concurrency limits and keeps 35 days to safely serve the 30-day contract.

The Console keeps build diagnostics separate from runtime observability and
provides an overview plus dedicated logs, metrics, and event views. Empty,
loading, partial, and unavailable states are explicit. Runtime metrics are
aggregate product signals and never expose Pod names. Runtime events expose
stable, sanitized product messages rather than raw Kubernetes object names,
UIDs, or messages. Traces, public dashboards, alerts, long retention, and backup
remain outside this decision.

Every AppEnvironment view carries a compact operational scoreboard. Metrics are
live only while that view is visible; historical log search is the default and
live tail is explicit. Unknown and stale telemetry remains distinguishable from
zero, and deployment events can be correlated with historical charts.
The browser keeps historical metrics separate from the live scoreboard. It
composes historical log pages and live batches in a dedicated bounded store,
merges sorted batches in linear time, publishes updates at a controlled cadence,
and virtualizes rendered rows.
It reports when old rows leave the local view and pauses automatic following when
the user scrolls away from the newest records.

## Consequences

Workspace isolation is enforced centrally and can be tested independently of
the storage engines. The ingestion and storage topology can scale or be replaced
without changing the API or Console. The initial laboratory stack is single
replica and pre-alpha; it does not claim HA, disaster recovery, long-term
retention, or hostile multi-tenant isolation.
