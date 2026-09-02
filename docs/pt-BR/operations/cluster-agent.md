# Operação do Cluster Agent

O Cluster Agent é um conector de saída pre-alpha. Ele ainda não reconcilia
workloads. O Operator pode ser instalado primeiro; o Agent inicia como
`Unpaired`, permanece ready e aguarda configuração sem abrir uma porta de
gerenciamento de entrada.

## Identidade da instalação

`molejoctl control-plane install` cria `molejo-agent-ca`, com `ca.crt` e
`ca.key`, e `molejo-agent-server-tls`, com `tls.crt` e `tls.key`, em
`molejo-control-plane`. O certificado de servidor cobre
`control-plane-api.molejo-control-plane.svc.cluster.local`. A chave da CA fica fora
do Git e é montada somente no pod da API.

O ServiceAccount do Agent pode apenas ler e atualizar
`molejo-agent-identity` e `molejo-agent-enrollment` em `molejo-system`. Ele não
lista Secrets nem acessa CRDs da Molejo. A chave privada é gerada pelo Agent e
nunca sai de `molejo-agent-identity`.

## Instalação no k3s

Instale Operator e Agent com `molejoctl cluster install` e depois execute
`molejoctl control-plane install`. O segundo comando cria ou reutiliza a CA
ECDSA P-256, o certificado interno, as credenciais do banco e o convite inicial
de enrollment. Ele instala PostgreSQL e API, configura os endpoints internos
HTTPS e gRPC e aguarda o Agent ficar `Paired`. As credenciais ficam em Secrets e
somente são impressas com `--show-generated-credentials`.

## Pareamento

O Agent inicial no mesmo cluster é pareado automaticamente pelo instalador. Para
Agents adicionais, um administrador chama `POST
/api/v1/admin/agent-installations` e transfere o token de uso único para
`molejo-agent-enrollment` sem colocá-lo no Git, histórico do shell, logs ou chat.

O Agent persiste chave, CSR e ID da tentativa antes do enrollment; timeout ou
crash repete a mesma operação. A resposta é validada contra a chave local, Agent
CA, validade e URI da instalação antes de ser persistida. Em seguida o token é
removido e o Agent abre o stream mTLS com TLS 1.3.

`/healthz` representa a saúde do processo. `/readyz` permanece disponível em
`Unconfigured`, `Unpaired`, `Enrolling`, `Connecting` e `Paired`; `/status`
expõe o estado sem material de identidade. `Failed` indica Secret de identidade
ilegível, não gravável, parcial, inválido ou certificado expirado.

Este corte não possui rotação automática. Um certificado de sete dias não deve
ser tratado como ciclo de vida de produção; refazer o pareamento é a recuperação
temporária pre-alpha.
