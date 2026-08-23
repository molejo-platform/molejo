# ADR-0002: Reconciliation State and Observability Contract

## Status

Draft

## Context

`AppDeployment` é um contrato assíncrono. Aplicar o Deployment desejado não
equivale a concluir um rollout, e o Kubernetes pode informar falhas transitórias,
persistentes ou semânticas por mecanismos diferentes. Se esses estados forem
interpretados dentro do código de I/O, a semântica do status, os retries, logs e
testes podem divergir conforme o operator evolui.

O diagnóstico operacional também exige sinais correlacionados sem vincular o
operator a um backend específico de monitoramento ou tracing.

## Decision

O operator avaliará uma máquina de estados interna e pura a partir de um snapshot
observado. Ela produz exatamente um estado ativo: `Ready`, `Progressing` ou
`Degraded`. Um rollout estará pronto somente quando o Deployment tiver observado
sua geração, todas as réplicas desejadas forem atuais e disponíveis, nenhuma
réplica antiga permanecer e não houver réplicas indisponíveis. Conditions de
falha do Deployment influenciam a decisão somente depois que ele observar sua
geração atual; Conditions de uma geração anterior são obsoletas e o rollout
permanece `Progressing`.

Os reasons públicos atuais formam o conjunto fechado `DeploymentProgressing`,
`DeploymentAvailable`, `ProgressDeadlineExceeded`, `ReplicaFailure`,
`OwnershipConflict`, `ReconcileFailed`, `HTTPRouteProgressing`,
`HTTPRouteRejected`, `GatewayProgressing`, `GatewayRejected` e
`HostnameConflict`. Os reasons específicos de publicação ampliam o contrato
original de estado do workload e são definidos pela ADR-0004. Conflitos de
ownership são bloqueios semânticos: atualizam o status, preservam a última release
observada com sucesso e usam requeue de cinco minutos sem retornar erro. Erros
persistentes da API Kubernetes usam `ReconcileFailed` com mensagem sanitizada no
status, preservam os campos observados e usam o mesmo requeue limitado. Erros
transitórios da API Kubernetes retornam erro e usam o backoff do
controller-runtime. Falhas informadas pelo status atual do Deployment atualizam
as Conditions sem retry artificial.

Conditions são o estado público durável. Kubernetes Events explicam transições
relevantes. Logs estruturados fornecem detalhe técnico, métricas Prometheus de
cardinalidade limitada fornecem agregação e spans OpenTelemetry fornecem tempo e
causalidade. As métricas são servidas com TLS, autenticação e autorização
Kubernetes. A exportação OTLP é opcional e configurada por variáveis padrão.
Detalhes técnicos dos erros permanecem em logs e traces correlacionados e não são
copiados para o status público.

## Consequences

A semântica de estado pode ser testada independentemente do I/O Kubernetes, e os
adapters podem mudar sem redefinir prontidão. Reasons públicos, nomes de métricas,
nomes de spans e cardinalidade de labels tornam-se contratos sensíveis à
compatibilidade.

O operator passa a depender de OpenTelemetry e da autenticação delegada do
Kubernetes. Falhas na exportação de traces não podem interromper a reconciliação,
e operadores devem conceder explicitamente acesso a `/metrics` aos consumidores.

## Alternatives Considered

Derivar prontidão somente de réplicas disponíveis. Essa alternativa foi rejeitada
porque um rolling update ainda pode servir réplicas antigas.

Retornar todo estado degradado como erro de reconciliação. Essa alternativa foi
rejeitada porque bloqueios semânticos persistentes criariam retries rápidos e
ruidosos.

Instalar uma stack de monitoramento e tracing com o operator. Essa alternativa
foi rejeitada porque coleta e armazenamento são decisões da implantação e não são
necessários para o contrato da plataforma.

## References

- [Status de Deployment no Kubernetes](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#deployment-status)
- [Proteção de métricas no Kubebuilder](https://book.kubebuilder.io/reference/metrics)
- [Exporters OpenTelemetry para Go](https://opentelemetry.io/docs/languages/go/exporters/)
- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
