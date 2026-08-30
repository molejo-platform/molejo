# ADR-0012: Bounded Multiport and Publication Contract

Status: Draft

## Context

Aplicações podem escutar em mais de uma porta e alguns protocolos não podem usar
um proxy reverso HTTP. Expor Services, NodePorts, listeners do Gateway ou load
balancers do provedor como configuração de produto acoplaria usuários a uma
instalação e levaria disputas de alocação para a API pública.

## Decision

Cada configuração de AppEnvironment declara de uma a oito portas TCP nomeadas do
container. Probes de startup, readiness e liveness referenciam uma porta pelo
nome e podem usar HTTP ou TCP. A publicação é uma lista limitada e separada, com
no máximo um endpoint HTTP e um endpoint TCP experimental.

O control plane, nunca o cliente, aloca portas TCP externas. O PostgreSQL mantém
claims transacionais de hostnames e portas externas e registra as versões de
configuração desejada e atual. Implantar uma configuração histórica reserva seus
claims novamente antes de criar o snapshot imutável do Deployment. A API nunca
aceita uma porta externa escolhida pelo cliente.

O Operator projeta a intenção em um único Service ClusterIP. A publicação HTTP
usa HTTPRoute e a TCP usa TCPRoute contra listeners nomeados no Gateway
compartilhado. O status é informado por endpoint. A instalação anuncia uma
capacidade TCP exata e fornece privadamente o pool limitado de listeners,
NodePorts, proxy e firewall.

## Consequences

O contrato do produto permanece portátil entre provedores Kubernetes, enquanto o
laboratório pode expor PostgreSQL, Redis, RabbitMQ e serviços TCP similares. A
capacidade é explícita e fail-closed: exaustão ou conflito retorna um erro estável
em vez de tomar um endereço de outro recurso.

O primeiro incremento suporta apenas TCP, alocação automática, uma publicação
HTTP e uma TCP por AppEnvironment e um pool de laboratório com 16 portas. Não
suporta UDP, roteamento TLS ou SNI, portas externas arbitrárias, múltiplas
publicações TCP, domínios customizados, claims compartilhados ou garantias de
disponibilidade de produção.

## References

- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Gateway API TCPRoute](https://gateway-api.sigs.k8s.io/api-types/tcproute/)
