# Operação do Platform Operator

Este runbook descreve os sinais expostos pelo controller de `AppDeployment`.
Conditions são a fonte durável da verdade; Events, logs, métricas e traces
explicam como o controller chegou ao estado atual.

## Contrato de estado

| Condition ativa | Significado | Primeiras verificações |
| --- | --- | --- |
| `Ready=True` | Service e o Deployment ou StatefulSet selecionado convergiram; todos os endpoints públicos também convergiram pelo Gateway compartilhado. | Confirme Service, workload, release observada, réplicas, Routes, Gateway e listener correspondente. |
| `Progressing=True` | O rollout do workload, a rota ou o Gateway compartilhado ainda está convergindo. | Inspecione o workload selecionado e, para workloads públicos, Conditions do parent da Route e do Gateway. |
| `Degraded=True` | Uma falha conhecida de workload, ownership, hostname, rota ou Gateway bloqueia a convergência. | Inspecione `reason`, Conditions dos filhos e do Gateway e Events. |

`DeploymentAvailable` e `StatefulSetAvailable` identificam estados prontos do
workload. Os reasons de progresso incluem `DeploymentProgressing`,
`StatefulSetProgressing`, `StatefulSetReplacingStalePod`, `HTTPRouteProgressing` e
`GatewayProgressing`. Os reasons estáveis de degradação são
`ProgressDeadlineExceeded`, `ReplicaFailure`, `OwnershipConflict`,
`HostnameConflict`, `HTTPRouteRejected`, `GatewayRejected`,
`PublicationRejected` e `ReconcileFailed`. Conflitos de ownership e hostname e
erros persistentes da API são verificados a cada cinco minutos sem usar o backoff
de erro do controller. Falhas transitórias da API Kubernetes usam o backoff de
erro do `controller-runtime`.

## Validação de schema

Campos desconhecidos são rejeitados quando o cliente solicita
`FieldValidation=Strict` no servidor. Nos modos `Warn` ou `Ignore`, o API server
do Kubernetes pode aceitar a requisição e remover os campos desconhecidos. A
instalação base não usa admission webhook para alterar esse comportamento do
Kubernetes.

## Contrato de runtime privado

Cada `AppDeployment` possui exatamente um workload e um Service ClusterIP com o
mesmo nome e namespace. O Service expõe de uma a oito portas TCP nomeadas do
container e permanece privado quando HTTPRoute ou TCPRoute publica portas
selecionadas. O spec exige digest imutável da imagem, requests e limits em
millicores de CPU e MiB e probes HTTP ou TCP de startup, readiness e liveness que
referenciam uma porta nomeada. Requests não podem superar limits.

O workload executa como não root, com seccomp `RuntimeDefault`, sem privilege
escalation nem capabilities e com root filesystem somente leitura. Startup possui
janela de 60 segundos; readiness executa a cada cinco segundos e liveness a cada
dez. O operator não lê Pods nem EndpointSlices; o status do workload permanece
como fonte do rollout.

## Contrato de publicação

`spec.publicEndpoints` contém no máximo uma publicação HTTP e uma TCP
experimental. HTTP conecta ao listener `https-molejo` e serve
o hostname exato resolvido pelo control plane a partir de `domainId` e
`hostnameLabel`. O catálogo inicial oferece `molejo.dev` aos dois tipos de
workload e `stateful.molejo.dev` somente a workloads Stateful. TCP conecta ao listener pré-alocado
`tcp-{externalPort}`. As duas rotas encaminham para uma porta nomeada do Service
de mesmo nome. Uma lista vazia mantém o workload privado. O Agent emite o contrato
atual de portas nomeadas, e a API rejeita portas ausentes ou probes incompletas.

O operator considera a publicação convergida somente quando o parent esperado da
rota possui Conditions `Accepted=True` e `ResolvedRefs=True` da geração atual, o
Gateway compartilhado possui `Programmed=True` atual e seu único listener `https-molejo`
possui `Accepted=True`, `Programmed=True` e `ResolvedRefs=True` atuais. Múltiplos
controllers reportando o mesmo parent efetivo da rota são ambíguos e mantêm a rota
em progresso. Estado ausente ou obsoleto do Gateway/listener reporta
`GatewayProgressing` enquanto o Gateway converge. Gateway ausente,
Gateway/listener atualmente rejeitado ou ausência de um único listener `https-molejo`
depois que o Gateway reporta `Programmed=True` reporta `GatewayRejected`. Uma
falha conhecida
do Deployment tem precedência sobre o progresso da publicação. Claims duplicados
de hostname convergem para um owner determinístico; o perdedor remove somente sua
própria rota, reporta `HostnameConflict`, preserva os campos da release observada e
tenta novamente após cinco minutos.

Remover um endpoint remove somente sua Route controlada sem remover workload ou
Service. Objetos em exclusão não são reconciliados, e o garbage collector do
Kubernetes trata seus filhos controlados.

## Fluxo de diagnóstico

Substitua o namespace e o nome de exemplo antes de executar:

```bash
kubectl get appdeployment ap-example -n ws-example -o yaml
kubectl describe appdeployment ap-example -n ws-example
kubectl get deployment ap-example -n ws-example -o yaml
kubectl describe deployment ap-example -n ws-example
kubectl get statefulset ap-example -n ws-example -o yaml
kubectl describe statefulset ap-example -n ws-example
kubectl get service ap-example -n ws-example -o yaml
kubectl get httproute ap-example -n ws-example -o yaml
kubectl describe httproute ap-example -n ws-example
kubectl get tcproute ap-example -n ws-example -o yaml
kubectl get gateway molejo -n molejo-system -o yaml
kubectl get pods -n ws-example -o wide
kubectl get events -n ws-example --sort-by=.metadata.creationTimestamp
kubectl logs deployment/platform-operator -n molejo-system --all-containers --prefix
```

Correlacione logs e traces por `trace_id` e refine a investigação com UID do
recurso, geração, estado e reason. Mensagens no status são sanitizadas de forma
intencional; erros técnicos permanecem nos logs e traces.

Events do workload, Service e Route identificam transições dos filhos. A aplicação
do Service e do HTTPRoute é rastreada pelos spans `kubernetes.service.apply` e
`kubernetes.httproute.apply`.
Reconciliações repetidas já convergidas não emitem Events de transição duplicados.

## Verificações reproduzíveis

`just molejo-conformance kind` cria um cluster Kind e um registry locais e
descartáveis, empacota os mesmos charts consumidos pelo `molejoctl` e valida a
instalação idempotente do runtime e do Control Plane. A jornada cria um Workspace
com limites de RBAC, registra uma imagem OCI imutável, reconcilia e observa uma
aplicação privada, cancela o stream de logs, remove a aplicação de forma
idempotente e comprova o teardown do ambiente.

Essa prova local não valida DNS público de entrada nem certificado publicamente
confiável. Esses itens permanecem como uma etapa de aceite separada no ambiente
da foundation.

## Saúde e métricas

Liveness informa a saúde do processo em `/healthz`. Readiness responde com
sucesso em `/readyz` somente após a sincronização do cache do manager. A
instalação base não expõe a porta de saúde por um Service.

## Encerramento gracioso

Ao receber `SIGTERM` ou `SIGINT`, readiness falha imediatamente e o manager tem
até 20 segundos para encerrar controllers, caches e servidores internos. Em
seguida, tracing usa um contexto novo para um flush limitado a cinco segundos. O
Pod concede 30 segundos no total, preservando uma margem para a saída do processo
antes que o Kubernetes possa forçar o encerramento.

Uma reconciliação interrompida pelo encerramento do processo é trabalho
incompleto, não degradação do workload. Ela não registra `ReconcileFailed`; o
estado desejado permanece durável e é retomado pela próxima instância do manager.
Um segundo sinal representa encerramento forçado explícito e não garante
drenagem nem exportação dos traces.

As métricas ficam disponíveis por HTTPS autenticado em
`platform-operator-metrics.molejo-system.svc:8443`. Consumidores precisam de um
binding para o ClusterRole `platform-operator-metrics-reader`. Além das métricas
nativas do controller-runtime, o operator expõe:

- `molejo_platform_operator_build_info`;
- `molejo_platform_operator_state_transitions_total`.

Nomes de recursos, namespaces, UIDs, digests de imagem e trace IDs são
deliberadamente excluídos das labels de métricas.

## Tracing opcional

Tracing vem desabilitado e nenhum collector é instalado. Configure o Deployment
com variáveis padrão do OpenTelemetry para habilitar exportação OTLP:

```yaml
env:
  - name: OTEL_TRACES_EXPORTER
    value: otlp
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: http://opentelemetry-collector.observability.svc:4318
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: http/protobuf
```

Configuração inválida impede o startup. A indisponibilidade do collector pode
descartar telemetria, mas não interrompe a reconciliação. Durante o encerramento,
o operator tenta descarregar os traces dentro de um limite de tempo.
