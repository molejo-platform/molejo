# ADR 0018: observação de capacidades e disponibilidade de features

## Status

Aceito para a arquitetura alpha.

## Contexto

A Molejo precisa explicar quais fluxos de aplicação estão utilizáveis sem tratar
uma distribuição Kubernetes, um produto cloud ou uma stack de terceiros como
feature do produto. Negociação de protocolo, fatos do cluster, configuração de
providers e o resultado apresentado ao desenvolvedor têm owners e ciclos de vida
diferentes.

## Decisão

A Molejo usa quatro contratos separados:

1. Uma **Protocol Capability** é uma string versionada trocada entre binários da
   Molejo para negociar comportamento opcional de wire. Ela não prova que o
   cluster ou provider subjacente está utilizável.
2. Uma **Capability Observation** é um fato limitado e datado, coletado pelo
   Cluster Agent autenticado. Ela descreve suporte e saúde sem modificar estado
   desejado.
3. Um **Provider Binding** é uma configuração tipada do control plane que conecta
   uma necessidade do produto a uma implementação externa. Não é um registry
   genérico de plugins e não expõe credenciais ou endpoints aos clientes.
4. **Feature Availability** é uma projeção read-only derivada pelo control plane
   a partir de estado de produto, observações recentes, negociação de protocolo e
   bindings de providers.

Identificadores expressam resultados neutros de provider, como
`runtime.workload.apply`, `runtime.logs.current` e
`telemetry.metrics.historical`. Nomes de providers ou distribuições são fatos ou
tipos de binding, nunca identificadores de feature.

A disponibilidade é escopada a Workspace, App ou AppEnvironment e possui um dos
estados `Available`, `Limited`, `NotConfigured`, `Unavailable`, `Unsupported` ou
`Unknown`. Reason codes estáveis explicam o resultado. Autorização de atores é
uma decisão separada e nunca é codificada como disponibilidade estrutural.

O horário de recebimento do control plane é autoritativo para freshness. Uma
observação expirada ou um Agent desconectado resulta em `Unknown`. Snapshots
completos substituem atomicamente o anterior; snapshots parciais atualizam apenas
as capacidades presentes.

Telemetria atual e histórica são capacidades diferentes. Dados atuais podem vir
diretamente do Kubernetes e são limitados e efêmeros. Dados históricos exigem um
provider com suas próprias garantias de retenção e consulta. Um não substitui o
outro silenciosamente.

## Ownership

- Cluster Agent observa fatos do cluster e os reporta por transporte autenticado.
- Control plane possui bindings, freshness, resolução e projeção pública.
- Platform Operator reconcilia somente recursos de runtime da Molejo.
- `molejoctl` pode inspecionar o mesmo vocabulário localmente, mas não sobrescreve
  observações do control plane.
- Console consome disponibilidade e a combina com autorização apenas na
  apresentação.

## Consequências

O Console pode explicar fluxos indisponíveis antes que uma integração falhe,
fatos de um cluster não satisfazem outro e providers opcionais continuam
componíveis. Observações nunca instalam infraestrutura, rotacionam credenciais ou
modificam estado desejado.

## Referências

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [ADR 0016: política de ciclo de vida alpha](0016-politica-de-ciclo-de-vida-alpha.md)
- [ADR 0021: bindings explícitos gerenciados pelo operador](0021-bindings-explicitos-gerenciados-pelo-operador.md)
- [ADR 0022: métricas neutras com consulta Prometheus-compatible](0022-metricas-neutras-com-consulta-prometheus.md)
