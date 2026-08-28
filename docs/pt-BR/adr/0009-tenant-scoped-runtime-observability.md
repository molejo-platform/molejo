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
seletores de tenant. Consultas históricas ficam limitadas a 24 horas e volumes de
resultado definidos; logs live usam SSE autenticado com limites de concorrência
por ator e duração. A autorização é revalidada durante o stream.

OpenTelemetry Collectors formam a fronteira portátil de ingestão. Um agente por
nó coleta logs de containers e métricas do kubelet, enquanto um collector de
cluster reúne eventos do Kubernetes. Um gateway enriquece e exporta os sinais. A
implantação de laboratório usa ClickHouse para logs/eventos de curta duração e
VictoriaMetrics para métricas, protegidos por NetworkPolicies e Services
internos. Eles são adapters substituíveis de implantação, não contratos públicos
do produto. A disponibilidade das aplicações e a readiness do control plane não
dependem da disponibilidade do armazenamento de telemetria.

A Console mantém diagnóstico de build separado da observabilidade de runtime e
oferece um resumo e visões próprias de logs, métricas e eventos. Estados vazios,
de carregamento, parciais e indisponíveis são explícitos. Eventos de runtime
expõem mensagens estáveis e sanitizadas do produto, nunca nomes de objetos, UIDs
ou mensagens brutas do Kubernetes. Traces, dashboards públicos, alertas, retenção
longa e backup permanecem fora desta decisão.

## Consequences

O isolamento por Workspace é aplicado centralmente e pode ser testado sem
depender dos engines de armazenamento. A topologia de ingestão e armazenamento
pode escalar ou ser substituída sem mudar a API ou a Console. A stack inicial do
laboratório é single-replica e pre-alpha; ela não declara HA, recuperação de
desastres, retenção longa ou isolamento contra tenants hostis.
