# ADR 0022: Provider-neutral metrics with a Prometheus-compatible query adapter

## Status

Accepted for the alpha architecture.

## Context

Developers need a stable and approachable metrics experience, while Cluster
Operators need to keep a monitoring stack they already understand and trust.
Making PromQL or a vendor API part of the Molejo product contract would couple
applications and the Console to infrastructure choices. Ignoring the Prometheus
ecosystem would instead force operators to replace widely adopted infrastructure.

The 2025 CNCF Annual Survey reports Prometheus in production at 77% of respondents
and under evaluation at another 12%. Prometheus-compatible query APIs are also
provided by managed and self-hosted systems including Amazon Managed Service for
Prometheus, Google Cloud Managed Service for Prometheus, Thanos, Grafana Mimir,
and VictoriaMetrics.

## Decision

The Molejo product port is provider-neutral and named for its consumer:
`HistoricalMetricReader`. It receives a bounded Molejo scope and product query
and returns normalized series. Public APIs and the Console expose product
concepts such as CPU, memory, request rate, error rate, and latency; they do not
expose PromQL, provider URLs, tenant headers, or provider credentials.

The first technical implementation is a `PrometheusQueryAdapter` over the stable
subset of the Prometheus HTTP query API required by Molejo, initially instant and
range queries. The current vendor-named `VictoriaMetricsClient` is renamed to
describe this actual protocol contract. A compatible backend may satisfy the
adapter only after a conformance probe verifies authentication, query behavior,
freshness, required Molejo identity labels, and cluster/Workspace isolation.

Authentication and tenancy remain concrete adapter configuration concerns. AWS
SigV4, Google credentials, bearer tokens, basic authentication, and tenant
headers are not flattened into the product domain. Provider-specific adapters
are added only when Molejo consumes behavior outside the compatible subset.
VictoriaMetrics therefore uses the compatible adapter initially; a dedicated
adapter is justified only by MetricQL or another VictoriaMetrics-specific
contract.

Current Kubernetes resource metrics remain a separate
`runtime.metrics.current` capability backed by `metrics.k8s.io`. Historical
metrics never fall back to that ephemeral source. OpenTelemetry is an optional
instrumentation and transport path and does not replace the historical query
port.

The binding is created explicitly under ADR 0021. Installing or operating a
Prometheus stack is not part of this adapter and remains an operator-owned
capability runbook.

## Consequences

- Developers receive a provider-independent metrics model without learning
  PromQL for routine application operations.
- Operators can reuse Prometheus, managed Prometheus services, or compatible
  systems instead of adopting a Molejo-specific stack.
- Prometheus compatibility is an honest adapter boundary, not falsely presented
  as a universal metrics-provider abstraction.
- Provider-specific authentication and extensions can evolve without changing
  the application API.
- Supporting a compatible endpoint still requires Molejo label-schema and
  isolation conformance; HTTP compatibility alone is insufficient.

## Alternatives Considered

Exposing PromQL publicly was rejected because it leaks provider syntax and makes
the developer experience infrastructure-specific. Keeping a
`VictoriaMetricsClient` as the primary abstraction was rejected because the
implemented query contract is broader than that vendor. Requiring Molejo to
install Prometheus was rejected because it would cross the product's cluster
ownership boundary. A universal metrics adapter was rejected because query,
authentication, tenancy, and semantic compatibility cannot be assumed across
all telemetry products.

## References

- [ADR 0018: Capability observation and feature availability](0018-capability-observation-and-feature-availability.md)
- [ADR 0021: Explicit operator-managed bindings](0021-explicit-operator-managed-bindings.md)
- [2025 CNCF Annual Survey](https://www.cncf.io/wp-content/uploads/2026/01/CNCF_Annual_Survey_Report_final.pdf)
- [Kubernetes resource and full metrics pipelines](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-usage-monitoring/)
- [Prometheus overview](https://prometheus.io/docs/introduction/overview/)
- [OpenTelemetry and Prometheus compatibility](https://opentelemetry.io/docs/compatibility/prometheus/)
- [Amazon Managed Service for Prometheus](https://docs.aws.amazon.com/prometheus/)
- [Google Cloud Managed Service for Prometheus](https://docs.cloud.google.com/stackdriver/docs/managed-prometheus)
- [VictoriaMetrics Prometheus querying API](https://docs.victoriametrics.com/victoriametrics/#prometheus-querying-api-usage)
