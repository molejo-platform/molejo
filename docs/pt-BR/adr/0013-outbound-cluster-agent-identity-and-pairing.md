# ADR-0013: Outbound Cluster Agent Identity and Pairing

Status: Accepted

## Context

O Operator reconcilia a intenção de runtime sem ficar exposto a um control plane
remoto. Uma instalação portátil ainda precisa de uma conexão estreita que
atravesse os limites de cluster e rede sem entregar uma credencial Kubernetes à
API pública nem exigir uma porta de gerenciamento de entrada no cluster.

## Decision

O Cluster Agent possui uma identidade de instalação e inicia um stream gRPC de
saída para o control plane. Ele gera uma chave ECDSA P-256 dentro do cluster,
persiste-a em um único Secret nomeado, faz enrollment com um token de uso único
válido por dez minutos e envia somente um CSR. Uma Agent CA fornecida pelo
instalador assina um certificado de cliente válido por sete dias cujo URI SAN é
`spiffe://molejo.dev/agent/{installationId}`.

A conexão gRPC exige TLS 1.3 e autenticação mútua. A identidade durável do Cluster
e suas credenciais rotativas são registros separados. Cluster declarado, URI,
fingerprint e registro no PostgreSQL precisam coincidir. Instalações novas usam
raízes separadas para cliente e servidor. Certificados cliente de sete dias são
renovados automaticamente com tentativa idempotente persistida e sobreposição de
uma hora. Administradores podem revogar o Cluster e todas as suas credenciais.

O protocolo versionado negocia capacidades e transporta comandos com versão de
schema, deadline, lease durável e fencing token. O Agent reporta somente
observações não sensíveis dos objetos de runtime pertencentes à Molejo. Estado
desejado, roteamento, auditoria e ciclo das operações permanecem autoritativos no
PostgreSQL. Vínculos Workspace-to-Cluster e o posicionamento do AppEnvironment
são explícitos.

## Consequences

O cluster mantém sua chave privada e não aceita tráfego de gerenciamento de
entrada. O control plane recebe um transporte autenticado e versionado sem se
tornar proprietário de credenciais Kubernetes. O pareamento pertence à
instalação e não é concedido por membership em Workspace.

O control plane pode reiniciar ou executar múltiplas réplicas sem perder o
fencing dos resultados, pois o comando ativo não fica em memória de processo. O
Agent pode reconectar com a credencial anterior durante a sobreposição, mas no
máximo uma operação viva é arrendada por Cluster. Adicionar um Cluster não move
workloads implicitamente. A reconciliação Kubernetes continua pertencendo ao
Platform Operator; o Agent aplica intenção contratada e reporta observação.

## Alternatives Considered

Embutir o conector na API pública acopla credenciais Kubernetes ao deployment do
control plane. Callbacks de entrada exigem exposição do cluster. Persistir a
chave privada no PostgreSQL transfere sua posse para fora do cluster. Essas
alternativas não foram escolhidas.

## References

- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Operação do Cluster Agent](../operations/cluster-agent.md)
