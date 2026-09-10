# Inspeção da foundation

`molejoctl foundation inspect --kube-context <contexto>` lê o cluster selecionado e
informa distribuição e versão do Kubernetes, nodes, StorageClass padrão,
disponibilidade da Gateway API e componentes Molejo instalados. Ele não altera o
cluster nem declara suporte formal a uma distribuição Kubernetes.

Use o relatório antes da instalação da plataforma e ao diagnosticar drift do
ambiente. Criação do cluster, ciclo de vida de nodes, CNI, IAM, DNS, firewall e load
balancer específicos do provedor permanecem fora da Molejo.
