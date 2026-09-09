# ADR 0022: métricas neutras com adapter de consulta Prometheus-compatible

## Status

Aceito para a arquitetura alpha.

## Contexto

Desenvolvedores precisam de uma experiência estável e acessível para métricas,
enquanto Cluster Operators precisam manter a stack de monitoramento que já
conhecem e confiam. Tornar PromQL ou uma API de vendor parte do contrato de
produto da Molejo acoplaria aplicações e Console às escolhas de infraestrutura.
Ignorar o ecossistema Prometheus forçaria operadores a substituir uma
infraestrutura amplamente adotada.

A pesquisa anual da CNCF de 2025 reporta Prometheus em produção para 77% dos
respondentes e em avaliação para outros 12%. APIs de consulta
Prometheus-compatible também são fornecidas por sistemas gerenciados e
self-hosted como Amazon Managed Service for Prometheus, Google Cloud Managed
Service for Prometheus, Thanos, Grafana Mimir e VictoriaMetrics.

## Decisão

A porta de produto da Molejo é neutra de provider e recebe o nome do consumidor:
`HistoricalMetricReader`. Ela recebe escopo e consulta de produto limitados e
retorna séries normalizadas. APIs públicas e Console expõem conceitos como CPU,
memória, taxa de requisições, taxa de erros e latência; não expõem PromQL, URLs,
headers de tenant ou credenciais de provider.

A primeira implementação técnica é um `PrometheusQueryAdapter` sobre o
subconjunto estável da API HTTP de consulta do Prometheus necessário à Molejo,
inicialmente consultas instantâneas e por intervalo. O atual
`VictoriaMetricsClient` será renomeado para descrever esse contrato de protocolo.
Um backend compatível somente satisfaz o adapter após uma prova de conformidade
validar autenticação, comportamento das consultas, freshness, labels de
identidade da Molejo e isolamento entre cluster e Workspace.

Autenticação e tenancy permanecem configuração concreta do adapter. AWS SigV4,
credenciais Google, bearer token, autenticação básica e tenant headers não são
achatados no domínio de produto. Adapters específicos entram apenas quando a
Molejo consumir comportamento fora do subconjunto compatível. VictoriaMetrics
usa inicialmente o adapter compatível; um adapter dedicado só se justifica por
MetricQL ou outro contrato específico.

Métricas atuais de recursos Kubernetes continuam como capability separada
`runtime.metrics.current`, apoiada por `metrics.k8s.io`. Métricas históricas nunca
fazem fallback para essa fonte efêmera. OpenTelemetry é um caminho opcional de
instrumentação e transporte, não substituto da porta de consulta histórica.

O binding é criado explicitamente conforme o ADR 0021. Instalar ou operar uma
stack Prometheus não faz parte do adapter e permanece um runbook de capability
sob responsabilidade do operador.

## Consequências

- Desenvolvedores recebem um modelo independente de provider sem aprender
  PromQL para operações comuns.
- Operadores reutilizam Prometheus, serviços gerenciados ou sistemas compatíveis
  sem adotar uma stack específica da Molejo.
- Compatibilidade Prometheus é um limite honesto de adapter, não uma abstração
  falsamente universal.
- Autenticação e extensões específicas evoluem sem alterar a API de aplicações.
- Um endpoint compatível ainda precisa provar schema de labels e isolamento;
  compatibilidade HTTP isolada é insuficiente.

## Alternativas consideradas

Expor PromQL publicamente foi rejeitado por vazar sintaxe de provider. Manter
`VictoriaMetricsClient` como abstração principal foi rejeitado porque o contrato
implementado é mais amplo que esse vendor. Exigir que a Molejo instale Prometheus
foi rejeitado por atravessar o limite de ownership do cluster. Um adapter
universal de métricas foi rejeitado porque consulta, autenticação, tenancy e
semântica não são equivalentes entre todos os produtos de telemetria.

## Referências

- [ADR 0018: observação de capacidades e disponibilidade](0018-observacao-de-capacidades-e-disponibilidade-de-features.md)
- [ADR 0021: bindings explícitos gerenciados pelo operador](0021-bindings-explicitos-gerenciados-pelo-operador.md)
- [Pesquisa anual CNCF 2025](https://www.cncf.io/wp-content/uploads/2026/01/CNCF_Annual_Survey_Report_final.pdf)
- [Pipelines de métricas do Kubernetes](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-usage-monitoring/)
- [Visão geral do Prometheus](https://prometheus.io/docs/introduction/overview/)
- [Compatibilidade entre OpenTelemetry e Prometheus](https://opentelemetry.io/docs/compatibility/prometheus/)
- [Amazon Managed Service for Prometheus](https://docs.aws.amazon.com/prometheus/)
- [Google Cloud Managed Service for Prometheus](https://docs.cloud.google.com/stackdriver/docs/managed-prometheus)
- [API de consulta Prometheus do VictoriaMetrics](https://docs.victoriametrics.com/victoriametrics/#prometheus-querying-api-usage)
