# ADR-0013: Outbound Cluster Agent Identity and Pairing

Status: Draft

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

A conexão gRPC exige TLS 1.3 e autenticação mútua. A instalação declarada, o URI
do certificado, o fingerprint e o registro no PostgreSQL precisam coincidir. A
primeira versão do protocolo troca apenas hello e heartbeat. O Agent não possui
permissão sobre AppDeployment nem acesso geral a Secrets e pode iniciar saudável
antes de existirem o control plane ou o token.

## Consequences

O cluster mantém sua chave privada e não aceita tráfego de gerenciamento de
entrada. O control plane recebe um transporte autenticado e versionado sem se
tornar proprietário de credenciais Kubernetes. O pareamento pertence à
instalação e não é concedido por membership em Workspace.

Este corte pre-alpha possui uma réplica e não inclui comandos, fila, banco local,
leader election, rotação automática, API de revogação, CLI ou fluxo no Console.
Os certificados expiram em sete dias; a rotação precisa ser implementada antes
de esta fronteira ser considerada operacionalmente durável.

## Alternatives Considered

Embutir o conector na API pública acopla credenciais Kubernetes ao deployment do
control plane. Callbacks de entrada exigem exposição do cluster. Persistir a
chave privada no PostgreSQL transfere sua posse para fora do cluster. Essas
alternativas não foram escolhidas.

## References

- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Operação do Cluster Agent](../operations/cluster-agent.md)
