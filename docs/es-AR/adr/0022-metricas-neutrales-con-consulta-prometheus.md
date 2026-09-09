# ADR 0022: métricas neutrales con adapter de consulta Prometheus-compatible

## Estado

Aceptado para la arquitectura alfa.

## Contexto

Los desarrolladores necesitan métricas accesibles y estables, mientras los
Cluster Operators necesitan conservar la stack de monitoreo que conocen. Exponer
PromQL o una API de vendor acoplaría el producto a infraestructura; ignorar el
ecosistema Prometheus obligaría a reemplazar una stack ampliamente adoptada.

La encuesta CNCF de 2025 reporta Prometheus en producción para 77% de los
encuestados y en evaluación para otro 12%. Amazon Managed Service for Prometheus,
Google Cloud Managed Service for Prometheus, Thanos, Grafana Mimir y
VictoriaMetrics también ofrecen APIs compatibles.

## Decisión

El puerto de producto es neutral y orientado al consumidor:
`HistoricalMetricReader`. Recibe un scope y una consulta limitada de Molejo y
retorna series normalizadas. La API pública y Console muestran CPU, memoria,
requests, errores y latencia; no exponen PromQL, URLs, tenant headers ni
credenciales.

La primera implementación es `PrometheusQueryAdapter` sobre el subconjunto
estable de la API HTTP de Prometheus necesario para queries instantáneas y de
rango. El actual `VictoriaMetricsClient` se renombra para representar este
contrato real. Un backend compatible debe probar autenticación, queries,
freshness, labels de identidad de Molejo y aislamiento entre clúster y Workspace.

Autenticación y tenancy permanecen detalles concretos del adapter. AWS SigV4,
credenciales Google, bearer tokens, basic auth y tenant headers no entran al
dominio de producto. VictoriaMetrics usa inicialmente el adapter compatible; un
adapter específico requiere una necesidad concreta como MetricQL.

`runtime.metrics.current`, respaldada por `metrics.k8s.io`, continúa separada y
nunca reemplaza métricas históricas. OpenTelemetry es una opción de
instrumentación y transporte, no el puerto de consulta histórica. El binding se
crea explícitamente según ADR 0021 y Molejo no instala Prometheus implícitamente.

## Consecuencias

- Desarrolladores no necesitan PromQL para operaciones comunes.
- Operadores reutilizan stacks Prometheus-compatible conocidas.
- El producto permanece neutral y la compatibilidad queda en un adapter honesto.
- Compatibilidad HTTP sin labels ni aislamiento no implica disponibilidad.

## Alternativas consideradas

PromQL público, un cliente primario con nombre VictoriaMetrics, instalación
obligatoria de Prometheus y un adapter universal fueron rechazados por acoplar el
producto o esconder diferencias reales de seguridad y semántica.

## Referencias

- [ADR 0018: observación de capacidades y disponibilidad](0018-observacion-de-capacidades-y-disponibilidad-de-features.md)
- [ADR 0021: bindings explícitos gestionados por el operador](0021-bindings-explicitos-gestionados-por-el-operador.md)
- [Encuesta anual CNCF 2025](https://www.cncf.io/wp-content/uploads/2026/01/CNCF_Annual_Survey_Report_final.pdf)
- [Pipelines de métricas de Kubernetes](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-usage-monitoring/)
- [Prometheus](https://prometheus.io/docs/introduction/overview/)
- [Compatibilidad OpenTelemetry y Prometheus](https://opentelemetry.io/docs/compatibility/prometheus/)
- [Amazon Managed Service for Prometheus](https://docs.aws.amazon.com/prometheus/)
- [Google Cloud Managed Service for Prometheus](https://docs.cloud.google.com/stackdriver/docs/managed-prometheus)
- [API Prometheus de VictoriaMetrics](https://docs.victoriametrics.com/victoriametrics/#prometheus-querying-api-usage)
