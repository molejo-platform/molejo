# ADR-0011: Portable Stateful Runtime and Volume Lifecycle

Status: Draft

## Context

O `AppEnvironment` já controla branch e configuração de runtime de um App dentro
de um Environment. Algumas aplicações também exigem dados que sobrevivam a uma
nova release ou à recriação do workload. Expor PVCs, StorageClasses, drivers CSI,
nós, zonas ou identificadores de provider transformaria a infraestrutura
Kubernetes na API pública do produto e faria o mesmo fluxo variar entre clusters.

## Decision

`AppEnvironment.workloadKind` é obrigatório na criação e pode ser `Stateless` ou
`Stateful`. Ele não é alterado pelo contrato comum de update. A escolha pertence
à combinação App e Environment; portanto, o mesmo App pode ser Stateless em um
Environment e Stateful em outro. Todo snapshot imutável de Deployment registra o
tipo selecionado.

O `AppDeployment` interno continua sendo a intenção estável de runtime e usa uma
união discriminada de workload. Stateless cria um Deployment e rejeita attachments
persistentes. Stateful cria um StatefulSet, exige exatamente uma réplica e um
attachment ReadWriteOnce e preserva os contratos existentes de Service,
HTTPRoute, probes, recursos, configuração, segurança e observabilidade. Seu root
filesystem permanece somente leitura; apenas o mount declarado é gravável.

`AppVolume` é uma intenção namespaced separada pertencente ao AppEnvironment. Ele
é projetado em um PersistentVolumeClaim durável, mas não possui owner reference
para release ou workload. Releases e StatefulSets o referenciam e não podem
apagá-lo. Expansão ocorre somente para cima. Exclusão é uma operação distinta,
idempotente, auditada, protegida por concorrência otimista e rejeitada enquanto
um Deployment ativo referenciar o volume.

Usuários escolhem um `StorageProfile` do produto. As capabilities públicas contêm
somente nome, limites de tamanho, quota disponível, expansão, snapshot, backup e
semântica de durabilidade. A instalação liga privadamente esse profile a uma
StorageClass. O profile inicial é `persistent-standard`; trocar seu binding entre
storage local, EBS CSI ou DigitalOcean Block Storage não altera domínio, contrato
HTTP ou fluxo do Console.

PostgreSQL é a fonte autoritativa de intenção, versões, reserva de quota,
operações e metadados de auditoria. Kubernetes é autoritativo somente para o
estado observado do runtime. Estados e reasons públicos são sanitizados. Nomes de
claims, UIDs, storage classes, CSI handles, topologia e erros Kubernetes brutos
nunca são retornados.

## Consequences

O produto ganha um caminho stateful explícito sem duplicar seu modelo de entrega
e observabilidade nem se acoplar a um provider. O primeiro incremento é pre-alpha
e oferece intencionalmente apenas uma réplica, um volume, ReadWriteOnce, retenção
com preservação padrão e expansão somente para cima.

Esta decisão não oferece conversão entre tipos de workload, volumes compartilhados
ou multi-attach, snapshots, backup, restore, importação, clone, disponibilidade
multi-zone, failover ou garantias de produção. Profiles node-local devem ser
apresentados como durabilidade local ao nó, não como alta disponibilidade.

## References

- [ADR-0002: Reconciliation State and Observability Contract](0002-reconciliation-state-and-observability-contract.md)
- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [ADR-0008: Exact Source Builds and Immutable Releases](0008-exact-source-builds-and-immutable-releases.md)
- [ADR-0010: Immutable Platform Configuration Releases](0010-immutable-platform-configuration-releases.md)
