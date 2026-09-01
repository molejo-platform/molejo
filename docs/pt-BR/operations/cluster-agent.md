# Operação do Cluster Agent

O Cluster Agent é um conector de saída pre-alpha. Ele ainda não reconcilia
workloads. O Operator pode ser instalado primeiro; o Agent inicia como
`Unpaired`, permanece ready e aguarda configuração sem abrir uma porta de
gerenciamento de entrada.

## Identidade da instalação

O instalador fornece `required-external-agent-ca-secret`, com `ca.crt` e
`ca.key`, e `required-external-agent-server-tls`, com `tls.crt` e `tls.key`, em
`molejo-control-plane`. O certificado de servidor precisa cobrir
`control-plane-api.molejo-control-plane.svc.cluster.local`. A chave da CA fica fora
do Git e é montada somente no pod da API.

O ServiceAccount do Agent pode apenas ler e atualizar
`molejo-agent-identity` e `molejo-agent-enrollment` em `molejo-system`. Ele não
lista Secrets nem acessa CRDs da Molejo. A chave privada é gerada pelo Agent e
nunca sai de `molejo-agent-identity`.

## Release no k3s

O build da release do control plane publica o Agent como imagem `linux/amd64`
separada e imutável. `just control-plane-prepare-k3s` cria ou reutiliza a CA
ECDSA P-256 e o certificado interno de servidor, controlados pelo instalador e
fora do checkout, e aplica Secrets versionados. A renderização substitui os
nomes desses Secrets e o placeholder da imagem; o apply aguarda o Deployment do
Agent. Proteja o diretório externo da release, pois ele contém a chave da CA.

## Pareamento

Um administrador da instalação chama `POST
/api/v1/admin/agent-installations` com um nome. A resposta entrega uma única vez
um token de enrollment de 256 bits, válido por dez minutos. Transfira-o para a
chave `token` de `molejo-agent-enrollment` sem colocá-lo no Git, histórico do
shell, logs ou chat.

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
