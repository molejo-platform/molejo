# Application loop

O application loop começa depois que foundation e plataforma estão prontas. Um
desenvolvedor ou CI registra uma release de imagem imutável, altera o estado
desejado pela API da Molejo e observa a operação e o estado resultante da aplicação.
O control plane autoriza e registra a intenção; o Cluster Agent outbound a entrega;
o Platform Operator reconcilia os CRs da Molejo no Kubernetes.

Sistemas de build, registries e engines GitOps são produtores combináveis. Eles não
alteram diretamente os recursos Kubernetes controlados pela Molejo. A primeira
integração documentada é a [CI externa](external-ci.md).
