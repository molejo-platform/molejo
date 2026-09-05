# ADR-0014: limite de Release e deploy por CI externa

Status: Aceito

## Contexto

A Molejo deve funcionar com GitHub Actions, pipelines de provedores e builders
futuros dentro do cluster sem transformar nenhuma dessas opções no modelo do
produto. Uma imagem no registry ainda não é uma Release da Molejo, e alterar o
Kubernetes diretamente contorna autorização, auditoria e reconciliação.

## Decisão

A CI é responsável por checkout, build, verificações e push da imagem OCI. Em
seguida, registra o artefato imutável `repositório@sha256:digest` como Release do
App no control plane. Uma requisição separada e limitada ao App Environment
seleciona essa Release para deploy. O control plane continua sendo a autoridade
do estado desejado; o Cluster Agent apenas transporta comandos de runtime
versionados até o Platform Operator.

Automações usam um Principal ServiceAccount de primeira classe. Sua credencial
opaca é armazenada somente como hash, expira, pode ser revogada e é exibida uma
única vez. As permissões são `release.write` para um App e
`deployment.create` somente nos App Environments selecionados. O deploy preserva
`If-Match` e idempotência. A automação pode ler os environments autorizados e
somente as operações que ela própria solicitou.

O registro de Release independe do provedor. Origem e proveniência são metadados;
GitHub não é uma relação obrigatória do banco. A mesma chave idempotente repete
o mesmo payload e conflita com outro. Digests iguais podem aparecer em Apps ou
execuções diferentes. Releases são histórico imutável e não expiram por uma
contagem implícita por environment.

O BuildKit gerenciado permanece como adaptador que produz o mesmo registro de
Release. No futuro, identidade de workload como GitHub OIDC pode substituir o
token opaco sem alterar as APIs de Release e Deployment.

## Consequências

Pipelines compõem com a Molejo por uma API pequena e estável. Autenticação do
registry e image pull continuam sendo configuração do Kubernetes/runbook; o
control plane apenas aplica sua allowlist. Cluster Agent e Platform Operator não
recebem tokens de CI nem código específico de provedor.

Criar e revogar credenciais de automação fica inicialmente restrito a Owners do
Workspace ou Managers explícitos. A rotação é criar, atualizar o pipeline e
revogar a credencial anterior.

## Alternativas consideradas

Permitir patch direto no Kubernetes foi rejeitado por contornar a fonte de
verdade. Tratar toda execução externa como Build foi rejeitado por acoplar
Release ao provedor. Enviar credenciais do registry ao Agent ou Operator foi
rejeitado porque o Kubernetes já possui esse contrato.

## Referências

- [Operação com CI externa](../operations/external-ci.md)
- [ADR-0013: identidade e pareamento outbound do Cluster Agent](0013-outbound-cluster-agent-identity-and-pairing.md)
