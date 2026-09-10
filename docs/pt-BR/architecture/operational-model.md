# Modelo operacional

A Molejo gerencia aplicações sobre Kubernetes; ela não provisiona clusters nem é
dona de nodes, rede, IAM, DNS ou do control plane do provedor. O produto é dividido
em quatro áreas operacionais:

1. **Foundation** observa o substrato Kubernetes e seus pré-requisitos sem
   modificá-los.
2. **Platform lifecycle** instala e diagnostica os componentes da Molejo.
3. **Capabilities** reúne runbooks explícitos e inspecionáveis que ajudam o
   operador a conectar serviços Kubernetes ou de provedores à Molejo.
4. **Application loop** cobre o caminho do desenvolvedor entre uma release
   imutável, o estado desejado e o estado observado da aplicação.

Esse limite também define o vocabulário da CLI: `foundation`, `platform` e
`capability`. Operações do application loop permanecem na API e no Console; a CLI
só irá expô-las quando existir um fluxo concreto do operador.

Contratos da Molejo descrevem intenção de produto. Receitas de capacidades são
documentos locais, não CRDs nem uma segunda fonte da verdade. Uma receita deve
expor plano, ownership, entradas, verificação e implicações de teardown. Ela pode
usar uma ferramenta ou credencial escolhida pelo operador, mas não pode esconder
a criação do cluster ou a administração de nodes.

## Camadas de decisão de segurança

Disponibilidade estrutural, consentimento do Cluster Operator, autorização do
ator e admission da solicitação são decisões independentes. Uma observação do
cluster ou feature flag do Console pode explicar se um fluxo está tecnicamente
disponível; nenhuma delas concede permissão. O control plane autoriza e admite
novamente toda mutação.

O contrato desejado permite ao Cluster Operator habilitar o provisionamento de
Workspaces como `Disabled` ou `Namespaced` na instalação do Agent. Em
`Namespaced`, um placement em um cluster corresponde a um namespace da Molejo. O
control plane possui o Workspace lógico, o Agent transporta estado desejado
limitado e um domínio privilegiado separado cria namespace e RoleBindings fixos.

Armazenamento e entrega de secrets também são responsabilidades distintas.
Quando parâmetros secretos estão habilitados, um SecretValueStore externo é a
fonte durável. Secret Kubernetes versionado é o mecanismo inicial de última
milha, não vault ou fallback.

A release atual é alpha. Contratos e caminhos da CLI podem quebrar entre alphas.
A transição suportada é uma reinstalação experimental limpa, não upgrade in-place.
As verificações de ownership ainda impedem adoção ou sobrescrita de recursos
alheios.

Fatos de runtime coletados pelo Cluster Agent são Capability Observations, não
estado da Foundation. O control plane combina observações recentes, protocolo,
estado do produto e bindings tipados em uma Feature Availability read-only, que
nunca modifica estado desejado.

Telemetria atual e histórica são capacidades distintas. Dados atuais são
limitados e efêmeros; dados históricos dependem de provider e retenção próprios.

## Referências de segurança

- [Limite do produto e topologia de runtime](../../en/adr/0001-product-boundary-and-runtime-topology.md)
- [Placement de Workspace e limite de privilégios Kubernetes](../../en/adr/0004-workspace-placement-and-kubernetes-privilege-boundary.md)
- [Composição de capabilities e bindings explícitos](../../en/adr/0005-capability-composition-and-explicit-bindings.md)
- [Custódia e entrega de secrets](../../en/adr/0006-secret-custody-and-runtime-delivery.md)
- [Threat model de segurança](threat-model-de-seguranca.md)
