# ADR-0009: Tenant-Scoped Runtime Observability

Status: Draft

## Context

Usuários precisam de logs de runtime, métricas básicas e eventos operacionais sem
acesso ao Kubernetes ou aos backends de armazenamento. As consultas cruzam os
limites de Workspace, Project, App e AppEnvironment, enquanto os backends de
telemetria são componentes operacionais cuja disponibilidade e topologia podem
mudar independentemente dos workloads gerenciados.

## Decision

A API pública resolve a hierarquia completa do produto e injeta o Namespace e a
identidade do runtime confiáveis em toda consulta. Clientes não podem informar
seletores de tenant. Logs históricos ficam limitados a 24 horas, eventos a 7 dias
e métricas a 30 dias. A API escolhe uma resolução de no máximo 1.000 pontos por
série; logs live usam SSE autenticado com limites de concorrência por ator e
duração. Snapshots atuais de métricas usam um orçamento SSE autenticado
separado e intervalo de consulta alinhado à coleta. Ambos os streams enviam
heartbeats, expiram e revalidam autorização enquanto conectados. Streams de
métricas concorrentes para o mesmo AppEnvironment compartilham um snapshot de
curta duração em vez de multiplicar consultas aos backends. Respostas parciais
identificam explicitamente quais sinais estão indisponíveis.

Todo log armazenado recebe identificador e timestamp de ingestão estáveis. A
navegação histórica usa cursores opacos por chave sobre um snapshot fixo; o SSE
live envia lotes limitados cujo ID de evento é um cursor opaco retomável. A
reconexão por `Last-Event-ID` é, portanto, idempotente e não depende de comparar o
conteúdo das mensagens nem de uma única janela de polling.

OpenTelemetry Collectors formam a fronteira portátil de ingestão. Um agente por
nó coleta logs de containers e métricas do kubelet, enquanto um collector de
cluster reúne eventos do Kubernetes. Um gateway enriquece e exporta os sinais. A
implantação de laboratório usa ClickHouse para logs/eventos de curta duração e
VictoriaMetrics para métricas, protegidos por NetworkPolicies e Services
internos. Eles são adapters substituíveis de implantação, não contratos públicos
do produto. A disponibilidade das aplicações e a readiness do control plane não
dependem da disponibilidade do armazenamento de telemetria.
O agente descarta sinais sem a label do AppEnvironment gerenciado, e o collector
de cluster mantém somente o Namespace de runtime gerenciado. Métricas de saúde
dos collectors seguem por um pipeline interno separado. O ClickHouse promove
Namespace e runtime confiáveis a colunas materializadas tipadas com índices de
salto, enquanto o VictoriaMetrics impõe limites de duração, séries, pontos e
concorrência e mantém 35 dias para servir com margem o contrato de 30 dias.

A Console mantém diagnóstico de build separado da observabilidade de runtime e
oferece um resumo e visões próprias de logs, métricas e eventos. Estados vazios,
de carregamento, parciais e indisponíveis são explícitos. Métricas são sinais
agregados do produto e nunca expõem nomes de Pods. Eventos de runtime
expõem mensagens estáveis e sanitizadas do produto, nunca nomes de objetos, UIDs
ou mensagens brutas do Kubernetes. Traces, dashboards públicos, alertas, retenção
longa e backup permanecem fora desta decisão.

Cada visão de AppEnvironment carrega um placar operacional compacto. Métricas
ficam live somente enquanto a visão está visível; a busca histórica de logs é o
padrão e o live tail é explícito. Telemetria desconhecida ou atrasada permanece
distinguível de zero, e eventos de implantação podem ser correlacionados com os
gráficos históricos.
A Console mantém as métricas históricas separadas do placar live. Ela compõe
páginas históricas de logs e lotes live em uma store dedicada e limitada, mescla
lotes ordenados em tempo linear, publica atualizações em cadência controlada e
virtualiza as linhas renderizadas. Ela informa quando registros antigos deixam a
visão local e pausa
o acompanhamento automático quando o usuário se afasta dos registros mais novos.

## Consequences

O isolamento por Workspace é aplicado centralmente e pode ser testado sem
depender dos engines de armazenamento. A topologia de ingestão e armazenamento
pode escalar ou ser substituída sem mudar a API ou a Console. A stack inicial do
laboratório é single-replica e pre-alpha; ela não declara HA, recuperação de
desastres, retenção longa ou isolamento contra tenants hostis.
