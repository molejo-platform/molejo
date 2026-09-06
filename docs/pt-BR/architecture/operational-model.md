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

A release atual é alpha. Contratos e caminhos da CLI podem quebrar entre alphas.
A transição suportada é uma reinstalação experimental limpa, não upgrade in-place.
As verificações de ownership ainda impedem adoção ou sobrescrita de recursos
alheios.
