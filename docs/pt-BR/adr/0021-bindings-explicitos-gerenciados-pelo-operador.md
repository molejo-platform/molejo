# ADR 0021: bindings explícitos gerenciados pelo operador

## Status

Aceito para a arquitetura alpha.

## Contexto

A Molejo deve compor com a infraestrutura em que o Cluster Operator já confia,
sem selecionar, instalar ou reconfigurar essa infraestrutura silenciosamente.
Observações de capabilities podem comprovar que uma StorageClass, um Gateway ou
um endpoint de telemetria existe, mas um candidato observado não representa
consentimento para utilizá-lo nem configuração durável do produto.

## Decisão

O Control Plane possui bindings de capabilities duráveis e tipados. Um Cluster
Operator cria, altera ou remove um binding explicitamente por um fluxo autenticado
de operação. O `molejoctl` pode conduzir esse fluxo e executar validações locais,
mas não torna um candidato observado efetivo sem a intenção do operador.

O Cluster Agent reporta evidências autenticadas e recentes sobre o cluster
selecionado e os recursos referenciados pelo binding. Ele nunca escolhe nem cria
um binding no Control Plane. Discovery pode apresentar candidatos, mas a ativação
permanece explícita.

Cada capability recebe um contrato estreito de binding quando sua primeira fatia
concreta for implementada, como métricas históricas, storage ou publicação. A
Molejo não introduz registry universal de providers, JSON arbitrário de
configuração ou lifecycle genérico de plugins. Credenciais de providers ficam
fora das respostas públicas e são representadas somente pelo mecanismo de
custódia do binding concreto.

Feature Availability combina o estado durável do binding com evidências atuais
do Agent. Um binding configurado sem prova recente é `Unknown`; saudável e
conforme é `Available`; inacessível é `Unavailable`; e ausência de binding é
`NotConfigured`.

## Consequências

- Observação da infraestrutura não modifica configuração do produto
  silenciosamente.
- Cluster Operators mantêm controle sobre qual stack existente a Molejo utiliza.
- Console e desenvolvedores recebem disponibilidade estável sem credenciais de
  provider ou detalhes de administração do cluster.
- O `molejoctl` permanece um runbook explícito e utilitário do operador, não um
  control plane implícito.
- Adicionar um provider exige contrato concreto de capability e evidência de
  conformidade, não registro em uma abstração universal.

## Alternativas consideradas

Vincular automaticamente o primeiro recurso descoberto foi rejeitado porque
discovery não é consentimento e ordenação não é política. Manter bindings somente
no cluster foi rejeitado porque o Control Plane não conseguiria tomar decisões
estáveis e multi-cluster de placement. Uma tabela universal de providers foi
rejeitada porque esconderia semântica, segurança e evidências de saúde específicas
de cada capability.

## Referências

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [ADR 0018: observação de capacidades e disponibilidade de features](0018-observacao-de-capacidades-e-disponibilidade-de-features.md)
