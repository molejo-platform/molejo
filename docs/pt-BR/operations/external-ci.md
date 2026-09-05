# Releases por CI externa

A CI externa constrói e envia a imagem; a Molejo registra e faz deploy do
resultado imutável. A integração nunca altera o Kubernetes diretamente.

## Criar uma credencial de automação

Um Owner do Workspace cria um service account do App em
`POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/apps/{appId}/service-accounts`.
A requisição escolhe os App Environments que podem receber deploy. A resposta
exibe o token uma única vez; guarde-o como secret mascarado da CI e nunca faça
commit. Na rotação, crie a substituta antes de revogar a conta antiga.

## Contrato da pipeline

1. Construa e envie a imagem OCI.
2. Resolva o digest e registre `repositório@sha256:digest` usando uma
   `Idempotency-Key` estável para aquela execução.
3. Leia o App Environment para obter `version`, `configurationVersion` e
   `currentDeploymentId`.
4. Solicite o deploy com outra chave idempotente e `If-Match` igual à versão do
   environment.
5. Opcionalmente acompanhe a Operation retornada. Conflito de versão significa
   que outro ator alterou o estado desejado; releia e decida explicitamente se
   deve tentar de novo.

O control plane rejeita tags mutáveis e registries fora de
`MOLEJO_ALLOWED_REGISTRIES`. Credenciais de image pull continuam sendo
configuração Day Zero do cluster e não trafegam nesta API.

O workflow completo de referência está em
[`docs/examples/github-actions-external-release.yml`](../../examples/github-actions-external-release.yml).
