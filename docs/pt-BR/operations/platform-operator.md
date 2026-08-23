# Operação do Platform Operator

Este runbook descreve os sinais expostos pelo primeiro controller de
`AppDeployment`. Conditions são a fonte durável da verdade; Events, logs,
métricas e traces explicam como o controller chegou ao estado atual.

## Contrato de estado

| Condition ativa | Significado | Primeiras verificações |
| --- | --- | --- |
| `Ready=True` | Service e Deployment convergiram; um workload público também possui HTTPRoute atual e aceito e Gateway HTTPS compartilhado programado. | Confirme Service, release observada, réplicas, parent do HTTPRoute, Gateway e listener `https`. |
| `Progressing=True` | O rollout do workload, a rota ou o Gateway compartilhado ainda está convergindo. | Inspecione Deployment e, para workloads públicos, Conditions do parent do HTTPRoute e do Gateway. |
| `Degraded=True` | Uma falha conhecida de workload, ownership, hostname, rota ou Gateway bloqueia a convergência. | Inspecione `reason`, Conditions dos filhos e do Gateway e Events. |

`DeploymentAvailable` identifica o estado pronto. Os reasons de progresso são
`DeploymentProgressing`, `HTTPRouteProgressing` e `GatewayProgressing`. Os reasons
estáveis de degradação são `ProgressDeadlineExceeded`, `ReplicaFailure`,
`OwnershipConflict`, `HostnameConflict`, `HTTPRouteRejected`, `GatewayRejected` e
`ReconcileFailed`. Conflitos de ownership e hostname e erros persistentes da API
são verificados a cada cinco minutos sem usar o backoff de erro do controller.
Falhas transitórias da API Kubernetes usam o backoff de erro do
controller-runtime.

## Validação de schema

Campos desconhecidos são rejeitados quando o cliente solicita
`FieldValidation=Strict` no servidor. Nos modos `Warn` ou `Ignore`, o API server
do Kubernetes pode aceitar a requisição e remover os campos desconhecidos. A
instalação base não usa admission webhook para alterar esse comportamento do
Kubernetes.

## Contrato de runtime privado

Cada `AppDeployment` possui exatamente um Deployment e um Service ClusterIP com o
mesmo nome e namespace. O Service aponta para a porta nomeada `http` do container
e permanece privado mesmo quando um HTTPRoute publica o workload. O spec exige
digest imutável da imagem, porta,
requests e limits em millicores de CPU e MiB, além dos paths de liveness e
readiness. Requests não podem superar limits.

O workload executa como não root, com seccomp `RuntimeDefault`, sem privilege
escalation nem capabilities e com root filesystem somente leitura. Startup usa o
path de readiness com janela de 60 segundos; readiness executa a cada cinco
segundos e liveness a cada dez. O operator não lê Pods nem EndpointSlices; o
status do Deployment permanece como fonte do rollout.

## Contrato de publicação

`spec.exposure` possui default `Private`. Um workload privado não possui
HTTPRoute e permanece acessível pelo Service ClusterIP. Um workload público exige
um label DNS em `spec.slug` e possui um HTTPRoute com o mesmo nome para
`{slug}.fruto.calouro.tech`. A rota se conecta ao listener `https` do Gateway
compartilhado `fruto`, em `fruto-system`, e encaminha para o Service de mesmo nome.

O operator considera a publicação convergida somente quando o parent esperado da
rota possui Conditions `Accepted=True` e `ResolvedRefs=True` da geração atual, o
Gateway compartilhado possui `Programmed=True` atual e seu único listener `https`
possui `Accepted=True`, `Programmed=True` e `ResolvedRefs=True` atuais. Múltiplos
controllers reportando o mesmo parent efetivo da rota são ambíguos e mantêm a rota
em progresso. Estado ausente ou obsoleto do Gateway/listener reporta
`GatewayProgressing` enquanto o Gateway converge. Gateway ausente,
Gateway/listener atualmente rejeitado ou ausência de um único listener `https`
depois que o Gateway reporta `Programmed=True` reporta `GatewayRejected`. Uma
falha conhecida
do Deployment tem precedência sobre o progresso da publicação. Claims duplicados
de hostname convergem para um owner determinístico; o perdedor remove somente sua
própria rota, reporta `HostnameConflict`, preserva os campos da release observada e
tenta novamente após cinco minutos.

Alterar um workload para `Private` remove seu HTTPRoute controlado sem remover
Deployment ou Service. Objetos em exclusão não são reconciliados, e o garbage
collector do Kubernetes trata seus filhos controlados.

## Fluxo de diagnóstico

Substitua o namespace e o nome de exemplo antes de executar:

```bash
kubectl get appdeployment ap-example -n ws-example -o yaml
kubectl describe appdeployment ap-example -n ws-example
kubectl get deployment ap-example -n ws-example -o yaml
kubectl describe deployment ap-example -n ws-example
kubectl get service ap-example -n ws-example -o yaml
kubectl get httproute ap-example -n ws-example -o yaml
kubectl describe httproute ap-example -n ws-example
kubectl get gateway fruto -n fruto-system -o yaml
kubectl get pods -n ws-example -o wide
kubectl get events -n ws-example --sort-by=.metadata.creationTimestamp
kubectl logs deployment/platform-operator -n fruto-system --all-containers --prefix
```

Correlacione logs e traces por `trace_id` e refine a investigação com UID do
recurso, geração, estado e reason. Mensagens no status são sanitizadas de forma
intencional; erros técnicos permanecem nos logs e traces.

Events `DeploymentCreated`, `DeploymentUpdated`, `ServiceCreated`,
`ServiceUpdated`, `HTTPRouteCreated`, `HTTPRouteUpdated` e `HTTPRouteDeleted`
identificam transições dos filhos. A aplicação do Service e da rota é rastreada
pelos spans `kubernetes.service.apply` e `kubernetes.httproute.apply`.
Reconciliações repetidas já convergidas não emitem Events de transição duplicados.

## Verificações reproduzíveis

`just e2e` constrói duas imagens da fixture localmente, carrega ambas em um
cluster Kind descartável e valida HTTP privado, egress interno controlado,
rollout por digest, falha e recuperação de probes, drift, recriação dos filhos e
garbage collection. Ele também instala um Gateway local e valida REST, GraphQL,
SSE incremental, WebSocket persistente, remoção da rota e TLS com um certificado
efêmero confiado pelo cliente de teste. `just e2e-public` adiciona uma chamada
HTTPS real de saída e fica deliberadamente fora do gate determinístico `just ci`.

Essa prova local não valida DNS público de entrada nem certificado publicamente
confiável. Esses itens permanecem como uma etapa de aceite separada no ambiente
da foundation.

## Contratos de imagem para frontend

As fixtures `static-html` e `vite-react-spa` são imagens de referência mantidas e
usam o mesmo runtime de AppDeployment. O operator também aceita imagens HTTP
imutáveis próprias e não inspeciona seu framework ou servidor. As referências
escutam em `8080`, expõem `/healthz` e `/readyz` e executam NGINX como
`65532:65532` com root filesystem somente leitura. O HTML estático retorna `404`
para paths desconhecidos. A SPA retorna `index.html` com HTTP `200` para rotas do
navegador; seu roteador no cliente é responsável pela página Not Found. Assets
ausentes retornam `404` e nunca recebem o shell da SPA. HTML usa `no-cache` e é
revalidado; assets com fingerprint são imutáveis por um ano.

Execute `just frontend-test` para provar o container restrito e `just e2e` para o
ciclo completo no Kind. Execute `just audit-frontend-images` separadamente quando
for necessária uma verificação pela base de vulnerabilidades do Docker Scout; a
auditoria mutável não integra o gate determinístico `just ci`.

`just e2e-frontend-k3s` é um aceite separado apenas para mantenedores. Ele exige
Docker com push autenticado pelo Buildx, `curl`, `jq`, `kubectl`, `sed`, cluster
somente amd64, Gateway `fruto-system/fruto` programado e o Secret de origem do
registry `fruto-system/registry-pull`. O alvo usa `--context fruto-lab` por default
e recusa outro contexto, salvo quando `FRUTO_KUBE_CONTEXT` e
`FRUTO_ALLOW_CUSTOM_CONTEXT=true` substituem explicitamente essa proteção.

O alvo publica três imagens amd64 com tags temporais, cria ou atualiza
`ws-phase4-static` e `ws-phase4-spa`, copia o Secret do registry para esses
namespaces, altera seus ServiceAccounts `default` e mantém os dois AppDeployments e
suas rotas públicas disponíveis. Arquivos locais temporários são removidos, mas
imagens do registry e recursos estáveis do cluster são preservados
intencionalmente. Ele nunca deve ser incluído em `just ci`.

## Saúde e métricas

Liveness informa a saúde do processo em `/healthz`. Readiness responde com
sucesso em `/readyz` somente após a sincronização do cache do manager. A
instalação base não expõe a porta de saúde por um Service.

As métricas ficam disponíveis por HTTPS autenticado em
`platform-operator-metrics.fruto-system.svc:8443`. Consumidores precisam de um
binding para o ClusterRole `platform-operator-metrics-reader`. Além das métricas
nativas do controller-runtime, o operator expõe:

- `fruto_platform_operator_build_info`;
- `fruto_platform_operator_state_transitions_total`.

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
