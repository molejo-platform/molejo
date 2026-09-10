# Platform Operator Operations

This runbook describes the signals exposed by the first `AppDeployment`
controller. Conditions are the durable source of truth; Events, logs, metrics,
and traces explain how the controller reached that state.

## State contract

| Active condition | Meaning | First checks |
| --- | --- | --- |
| `Ready=True` | The Service and Deployment converged; a public workload also has a current accepted HTTPRoute and a programmed shared HTTPS Gateway. | Confirm the Service, observed release, replicas, HTTPRoute parent, Gateway, and `https-molejo` listener. |
| `Progressing=True` | The workload rollout, route, or shared Gateway is still converging. | Inspect the Deployment and, for public workloads, HTTPRoute parent and Gateway conditions. |
| `Degraded=True` | A known workload, ownership, hostname, route, or Gateway failure blocks convergence. | Inspect `reason`, child and Gateway conditions, and Events. |

`DeploymentAvailable` identifies the ready state. Progressing reasons are
`DeploymentProgressing`, `HTTPRouteProgressing`, and `GatewayProgressing`. Stable
degraded reasons are `ProgressDeadlineExceeded`, `ReplicaFailure`,
`OwnershipConflict`, `HostnameConflict`, `HTTPRouteRejected`, `GatewayRejected`,
and `ReconcileFailed`. Ownership and hostname conflicts and persistent API errors
are checked every five minutes without using the controller error backoff.
Transient Kubernetes API failures use the controller-runtime error backoff.

## Schema validation

Unknown fields are rejected when the client requests server-side
`FieldValidation=Strict`. In `Warn` or `Ignore` mode, the Kubernetes API server
may accept the request and prune the unknown fields. The base installation does
not use an admission webhook to change this Kubernetes behavior.

## Private runtime contract

Each `AppDeployment` owns exactly one workload and one same-named ClusterIP
Service in its namespace. The Service exposes one to eight named TCP container
ports and remains private when HTTPRoute or TCPRoute publishes selected ports.
The spec requires an immutable image digest, resource requests and limits in CPU
millicores and MiB, and startup/readiness/liveness HTTP or TCP probes that
reference a named port. Requests must not exceed limits.

The workload runs as non-root with `RuntimeDefault` seccomp, no privilege
escalation or capabilities, and a read-only root filesystem. Startup has a
60-second window; readiness runs every five seconds and liveness every ten
seconds. The operator does not read Pods or EndpointSlices; workload status
remains the rollout source of truth.

## Publication contract

`spec.publicEndpoints` contains at most one HTTP and one experimental TCP
publication. HTTP attaches to `https-molejo` and serves
the exact hostname resolved by the control plane from `domainId` and
`hostnameLabel`. The initial catalog offers `molejo.dev` to both workload kinds
and `stateful.molejo.dev` only to Stateful workloads. TCP attaches to the preallocated
`tcp-{externalPort}` listener. Both routes forward to a named port on the
same-named Service. Empty publication keeps the workload private. Legacy
`spec.exposure`, `spec.slug`, and `spec.port` remain migration-only fields.

The operator considers publication converged only when the expected route parent
has current-generation `Accepted=True` and `ResolvedRefs=True` conditions, the
shared Gateway has current `Programmed=True`, and its single `https-molejo` listener has
current `Accepted=True`, `Programmed=True`, and `ResolvedRefs=True`. Multiple
controllers reporting the same effective route parent are ambiguous and keep the
route progressing. Missing or stale Gateway/listener state reports
`GatewayProgressing` while the Gateway converges. An absent Gateway, a currently
rejected Gateway/listener, or the absence of one unique `https-molejo` listener after the
Gateway reports `Programmed=True`, reports `GatewayRejected`. A known Deployment failure
takes precedence over publication progress. Duplicate hostname claims converge to
one deterministic owner; a loser removes only its own route, reports
`HostnameConflict`, preserves observed release fields, and retries after five
minutes.

Removing an endpoint removes only its owned Route without removing the workload
or Service. Objects already being deleted are not reconciled, and Kubernetes
garbage collection handles their owned children.

## Diagnostic workflow

Replace the example namespace and name before running these commands:

```bash
kubectl get appdeployment ap-example -n ws-example -o yaml
kubectl describe appdeployment ap-example -n ws-example
kubectl get deployment ap-example -n ws-example -o yaml
kubectl describe deployment ap-example -n ws-example
kubectl get service ap-example -n ws-example -o yaml
kubectl get httproute ap-example -n ws-example -o yaml
kubectl describe httproute ap-example -n ws-example
kubectl get tcproute ap-example -n ws-example -o yaml
kubectl get gateway molejo -n molejo-system -o yaml
kubectl get pods -n ws-example -o wide
kubectl get events -n ws-example --sort-by=.metadata.creationTimestamp
kubectl logs deployment/platform-operator -n molejo-system --all-containers --prefix
```

Correlate logs and traces using `trace_id`, then narrow the investigation with
the resource UID, generation, state, and reason. Status messages are intentionally
sanitized; technical errors remain in logs and traces.

`DeploymentCreated`, `DeploymentUpdated`, `ServiceCreated`, `ServiceUpdated`,
`HTTPRouteCreated`, `HTTPRouteUpdated`, and `HTTPRouteDeleted` Events identify
child transitions. Service and route application are traced by
`kubernetes.service.apply` and `kubernetes.httproute.apply`. Repeated converged
reconciliations do not emit duplicate transition Events.

## Reproducible checks

`just kubernetes-conformance kind` creates disposable local Kind and registry
instances, packages the same charts consumed by `molejoctl`, and validates the
idempotent runtime and Control Plane installation. The journey creates a
Workspace with RBAC boundaries, registers an immutable OCI image, reconciles and
observes a private application, cancels its log stream, deletes the application
idempotently, and proves environment teardown.

This local proof does not validate inbound public DNS or a publicly trusted
certificate. Those remain a separate acceptance step in the foundation
environment.

## Frontend image contracts

The `static-html` and `vite-react-spa` fixtures are maintained reference images
using the same AppDeployment runtime. The operator also accepts custom immutable
HTTP images and does not inspect their framework or server. The references listen
on `8080`, expose `/healthz` and `/readyz`, and run NGINX as `65532:65532` with a
read-only root filesystem. Static HTML returns `404` for unknown paths. The SPA
returns `index.html` with HTTP `200` for browser routes; its client router owns the
Not Found page. Missing assets return `404` and never receive the SPA shell. HTML
uses `no-cache` and is revalidated; fingerprinted assets are immutable for one
year.

Run `just frontend-test` for the restricted container proof and
`just kubernetes-conformance kind` for the complete Kind lifecycle. Run
`just audit-frontend-images` separately when a
Docker Scout vulnerability-database check is required; the mutable audit is not a
deterministic `just ci` gate.

`just e2e-frontend-k3s` is a separate maintainer-only acceptance target. It
requires Docker with an authenticated Buildx push, `curl`, `jq`, `kubectl`, `sed`,
an amd64-only cluster, a programmed `molejo-system/molejo` Gateway, and the source
registry Secret `molejo-system/registry-pull`. It defaults to `--context
molejo-lab` and refuses another context unless both `MOLEJO_KUBE_CONTEXT` and
`MOLEJO_ALLOW_CUSTOM_CONTEXT=true` explicitly override the guard.

The target pushes three timestamped amd64 images, creates or updates
`ws-e2e-static` and `ws-e2e-spa`, copies the registry Secret into them,
patches their default ServiceAccounts, and leaves both AppDeployments and their
public routes available. Temporary local files are removed, but registry images
and stable cluster resources are intentionally retained. It must never be added
to `just ci`.

## Health and metrics

Liveness reports process health at `/healthz`. Readiness reports success at
`/readyz` only after the manager cache is synchronized. The base installation
does not expose the health port through a Service.

## Graceful shutdown

On `SIGTERM` or `SIGINT`, readiness fails immediately and the manager has up to
20 seconds to stop controllers, caches, and internal servers. Tracing then uses
a fresh context for a bounded five-second flush. The Pod grants 30 seconds in
total so process exit retains a margin before Kubernetes can force termination.

A reconciliation interrupted by process shutdown is unfinished work, not
workload degradation. It does not record `ReconcileFailed`; the desired state
remains durable and is resumed by the next manager instance. A second signal is
an explicit forced termination and does not guarantee draining or trace export.

Metrics are available over authenticated HTTPS through
`platform-operator-metrics.molejo-system.svc:8443`. Consumers need a binding to
the `platform-operator-metrics-reader` ClusterRole. The operator exposes native
controller-runtime metrics plus:

- `molejo_platform_operator_build_info`;
- `molejo_platform_operator_state_transitions_total`.

Resource names, namespaces, UIDs, image digests, and trace IDs are deliberately
excluded from metric labels.

## Optional tracing

Tracing is disabled by default and no collector is installed. Configure the
Deployment with standard OpenTelemetry variables to enable OTLP export:

```yaml
env:
  - name: OTEL_TRACES_EXPORTER
    value: otlp
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: http://opentelemetry-collector.observability.svc:4318
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: http/protobuf
```

Invalid exporter configuration prevents startup. A collector outage may drop
telemetry but does not stop reconciliation. On shutdown, the operator attempts a
bounded trace flush.
