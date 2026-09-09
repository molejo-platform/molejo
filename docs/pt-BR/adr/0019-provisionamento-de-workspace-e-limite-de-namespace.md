# ADR 0019: provisionamento de Workspace e limite de namespace

## Status

Aceito para a arquitetura alpha.

## Contexto

Criar um Workspace é simultaneamente uma decisão de governança do produto e uma
solicitação para estabelecer um limite de execução no Kubernetes. Tratar uma
observação do cluster, uma feature flag do Console ou a posse da credencial do
Cluster Agent como autorização permitiria que configuração de infraestrutura
escalasse privilégios de produto.

Provisionar namespaces dinamicamente exige uma autoridade cluster-scoped. Ela não
pode ser eliminada enquanto o fluxo for self-service, mas pode ser isolada da
reconciliação de aplicações, dos valores de secrets e do tráfego público.

## Decisão

Toda solicitação de provisionamento passa por quatro gates independentes:

1. **Capability**: o cluster reporta suporte e saúde para provisionamento
   namespaced.
2. **Consentimento do cluster**: o Cluster Operator configura o Agent com o modo
   explícito `Disabled` ou `Namespaced`, somente pelo fluxo de instalação do
   cluster.
3. **Autorização**: o control plane verifica uma permissão atômica como
   `installation.workspace.create` para o ator autenticado.
4. **Admission**: uma política pura valida cluster, namespace, classe do
   Workspace, atribuição de owner, limites e idempotência antes de aceitar a
   operação.

Feature Availability pode descrever os dois primeiros gates, mas nunca concede o
terceiro nem ignora o quarto. A API sempre revalida autorização e admission.

No alpha, apenas Installation Administrator cria Workspace. No futuro, um
Workspace Provisioner não humano poderá receber a mesma permissão com restrições
explícitas de clusters, classes, owner, taxa e validade. Entradas humanas e
automatizadas chamam o mesmo caso de uso idempotente; integrações nunca chamam o
Cluster Agent diretamente.

Um placement de Workspace em um cluster corresponde a um namespace da Molejo. O
control plane possui o Workspace lógico e seu placement. Um CR cluster-scoped
`WorkspacePlacement` é sua projeção limitada e contém somente identidade
imutável, nome do namespace, perfil fixo de acesso e estado de ciclo de vida. Ele
não aceita manifests, verbos RBAC, role refs, ServiceAccounts, selectors ou
configuração de providers arbitrários.

O Cluster Agent pode reconciliar `WorkspacePlacement`, mas não cria Namespace ou
RBAC diretamente. Um reconciler de limites cria ou adota somente namespaces com
ownership compatível, rejeita namespaces reservados ou alheios e cria RoleBindings
para ClusterRoles fixas do Agent e do runtime Operator.

Provisionamento de limites e reconciliação de aplicações são domínios de
privilégio separados. Podem compartilhar o mesmo artefato binário, mas executam
em workloads e ServiceAccounts diferentes. O reconciler de limites não lê
Secrets, não cria workloads de aplicação e não se conecta ao control plane.

## Consequências

- Configuração do cluster não concede privilégios de produto.
- Uma credencial comprometida do Agent fica limitada aos namespaces vinculados.
- A criação passa a ser assíncrona, com conditions como `NamespaceReady`,
  `AgentAccessReady`, `OperatorAccessReady` e `PolicyReady`.
- O reconciler de limites permanece altamente confiável porque RBAC nativo não
  restringe todos os campos de um RoleBinding criado dinamicamente.
- Namespace reduz blast radius, mas não oferece isolamento forte contra workloads
  hostis no mesmo cluster ou node.

## Alternativas consideradas

Bindings cluster-wide foram rejeitados pelo blast radius. Dar Namespace e RBAC
diretamente ao Agent foi rejeitado por combinar execução remota, entrega de
secrets e concessão de privilégios. Feature flag client-side foi rejeitada como
controle de autorização. Exigir `molejoctl` para cada Workspace foi rejeitado por
impedir self-service administrativo e futuro provisionamento automatizado.

## Referências

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [ADR 0018: observação de capacidades e disponibilidade de features](0018-observacao-de-capacidades-e-disponibilidade-de-features.md)
- [Threat model de segurança da Molejo](../architecture/threat-model-de-seguranca.md)
- [Boas práticas de RBAC do Kubernetes](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
