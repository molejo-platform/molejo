# ADR-0004: Publication and HTTP Transport Contract

## Status

Draft

## Context

O backend stateless privado fornece um Service ClusterIP estável, mas não oferece
um endpoint público opcional. O próximo corte vertical precisa publicar o mesmo
backend por infraestrutura compartilhada da plataforma sem transformar DNS,
certificados, detalhes da implementação do Gateway ou autorização Kubernetes em
parte do contrato do produto. Ele também precisa preservar transportes HTTP de
longa duração e expor status determinístico quando workload e publicação mudam de
forma independente.

Unicidade de hostname não pode ser expressa como uma invariante local do schema
do CRD porque depende de outros objetos. O repositório atual também não possui API
de produto ou banco no qual impor uma restrição transacional de unicidade.

## Decision

`AppDeploymentSpec` usa o enum fechado `Private | Public`. `Private` é o default e
exige ausência do slug. `Public` exige um único label DNS minúsculo em `spec.slug`;
o hostname resultante é `{slug}.fruto.calouro.tech`.

Todo workload continua possuindo um Deployment e um Service ClusterIP com o mesmo
nome. Um workload público também possui um HTTPRoute com o mesmo nome em seu
namespace. A rota se conecta ao listener `https` do Gateway compartilhado `fruto`,
em `fruto-system`, e encaminha para o Service do workload. Retornar para `Private`
exclui somente o HTTPRoute controlado. O operator não cria Gateways, registros DNS
nem certificados.

Até existir um control plane do produto, o operator fornece ownership de hostname
determinístico e eventualmente consistente. Uma rota controlada já estabelecida
é preservada; caso contrário, timestamp de criação, nome com namespace e UID
desempatam. Se rotas duplicadas já existirem, o AppDeployment perdedor remove
somente sua própria rota e reporta `HostnameConflict`. Ele preserva os campos da
release observada e tenta novamente após cinco minutos. Um futuro control plane
deve substituir essa fronteira de alocação por uma restrição atômica de unicidade,
mantendo o comportamento externo.

Prontidão pública exige rollout completo do Deployment, Conditions atuais do
HTTPRoute para o parent esperado (`Accepted=True` e `ResolvedRefs=True`) e um
Gateway compartilhado atual. O Gateway deve reportar `Programmed=True`, e seu
único listener `https` deve reportar `Accepted=True`, `Programmed=True` e
`ResolvedRefs=True`. Múltiplos controllers reportando o mesmo parent efetivo da
rota são ambíguos e mantêm a rota em progresso. Conditions ausentes ou obsoletas
do Gateway/listener usam `GatewayProgressing` enquanto o Gateway converge. Gateway
ausente, Gateway/listener atual rejeitado ou ausência de um único listener `https`
depois que o Gateway reporta `Programmed=True` usa `GatewayRejected`. Uma falha
conhecida do workload tem precedência sobre o progresso da publicação. Falhas da
rota ou do Gateway produzem Conditions públicas sanitizadas sem expor erros
técnicos.

O teste end-to-end determinístico instala Gateway API e Traefik em um cluster
Kind descartável. Ele usa um certificado wildcard efêmero confiado pelo cliente
de teste e comprova REST, GraphQL, SSE incremental, WebSocket persistente, remoção
da rota e acesso contínuo pelo Service privado. Essa é evidência local de
transporte, não prova de DNS público ou certificado publicamente confiável. O
aceite externo da foundation permanece separado.

## Consequences

Workloads privados e públicos compartilham um único runtime e identidade de
Service. A publicação é reversível e não exige portas no host, acesso direto a
Pods nem mutação de DNS pelo operator. REST, GraphQL, SSE e WebSocket não precisam
de objetos de rota específicos porque usam o mesmo HTTPRoute e porta de backend.

A alocação de hostname é segura por convergência para o operator atual, com uma
réplica e em pre-alpha, mas executa uma consulta global de AppDeployments e não é
uma reserva transacional no domínio do produto. Maior concorrência do controller,
múltiplas réplicas e alocação em escala exigem um mecanismo de claim indexado ou
pertencente ao control plane.

Gateway, listener, sufixo de domínio e política de retry são política de plataforma
sensível à compatibilidade em `v1alpha1`. Domínios customizados, autenticação,
divisão de tráfego e ciclo de vida de certificados permanecem fora desta decisão.

## Alternatives Considered

Criar um Ingress por workload público. Essa alternativa foi rejeitada porque a
Gateway API fornece uma fronteira explícita de Gateway compartilhado e status
estruturado de rota.

Expor o Service como `LoadBalancer` ou `NodePort`. Essa alternativa foi rejeitada
porque contornaria a política HTTPS compartilhada e alocaria infraestrutura por
workload.

Permitir que usuários forneçam hostnames ou campos arbitrários de HTTPRoute. Essa
alternativa foi rejeitada porque exporia política de infraestrutura Kubernetes na
API do produto e ampliaria a superfície de compatibilidade prematuramente.

Exigir unicidade global transacional dentro do operator. Essa alternativa foi
rejeitada nesta fase porque operações Kubernetes de listagem e criação não
fornecem essa transação no domínio do produto. O operator converge projeções
duplicadas, enquanto o futuro control plane será responsável pela alocação
atômica.

Tratar TLS local bem-sucedido como prova de disponibilidade pública. Essa
alternativa foi rejeitada porque tráfego por port-forward no Kind não valida DNS
público, roteamento externo nem certificado de produção publicamente confiável.

## References

- [HTTPRoute da Gateway API](https://gateway-api.sigs.k8s.io/api-types/httproute/)
- [Status da Gateway API](https://gateway-api.sigs.k8s.io/guides/status/)
- [Owner references do Kubernetes](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
- [ADR-0002: Reconciliation State and Observability Contract](0002-reconciliation-state-and-observability-contract.md)
- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
