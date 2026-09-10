# Operação do Cluster Agent

O Cluster Agent é o único componente da Molejo que executa a intenção de runtime
do control plane no Kubernetes. Ele inicia um stream outbound com mTLS e TLS 1.3;
a API pública não recebe kubeconfig e o cluster não expõe porta de gerenciamento
de entrada.

## Instalação e confiança

Instale Operator e Agent com `molejoctl platform runtime install` e execute
`molejoctl platform control-plane install`. Uma instalação nova cria raízes ECDSA P-256
separadas:

- `molejo-agent-ca` assina identidades cliente dos Agents;
- `molejo-control-plane-server-ca` assina a identidade interna da API e do gRPC.

A separação impede que uma chave de servidor comprometida emita identidades de
Agent. Instalações alpha antigas que usam uma única CA não possuem caminho de
upgrade suportado; faça backup dos dados necessários e reinstale o alpha desejado.

O ServiceAccount do Agent possui acesso restrito aos dois Secrets nomeados de
identidade e aos recursos Kubernetes exigidos pelo contrato de runtime. Ele não
lista Secrets arbitrários. Secrets de configuração são selecionados e geridos
somente por nomes determinísticos derivados de ConfigMaps pertencentes à Molejo.

## Identidade do cluster e enrollment

Cluster é um registro durável do control plane. Suas credenciais são registros
filhos rotativos, não a identidade do próprio Cluster. Um administrador cria o
Cluster com `POST /api/v1/admin/clusters` e transfere o token de uso único, válido
por dez minutos, para `molejo-agent-enrollment` sem colocá-lo em Git, histórico
do shell, logs ou chat.

O Agent persiste chave, CSR e ID da tentativa antes do enrollment. Repetir a
mesma tentativa e CSR é idempotente. A chave privada nunca sai do cluster. Após
validar certificado, URI SAN, raízes de confiança e validade, o Agent remove o
token e abre seu stream outbound.

Certificados cliente duram sete dias e são renovados automaticamente nas últimas
24 horas. A renovação cria e persiste uma nova chave e CSR antes da requisição
autenticada. A credencial anterior permanece válida por uma sobreposição de uma
hora, permitindo convergência após interrupções. Repetir a mesma tentativa
retorna a mesma credencial; certificado novo e remoção do intento são persistidos
atomicamente. Revogar um Cluster invalida todas as credenciais e
falha operações enfileiradas ou arrendadas para ele.

A substituição da raiz de confiança é uma operação explícita em duas fases.
Antes dela, faça backup criptografado do PostgreSQL e dos Secrets
`molejo-agent-ca`, `molejo-control-plane-server-ca` e
`molejo-agent-server-tls`; nunca coloque esses backups no Git. Se uma CA for
perdida, restaure-a: o instalador não cria outra raiz sobre uma instalação ativa.

Na fase de transição, cada bundle contém a raiz nova primeiro e a antiga depois,
enquanto a chave ativa já pertence à raiz nova. O servidor aceita certificados
cliente de ambas, mas emite somente pela nova. O certificado servidor antigo é
mantido inicialmente, e as respostas de enrollment e renovação entregam ambos os
bundles. O hello anuncia um `trustBundleId`; se ele divergir do valor persistido,
o Agent renova imediatamente, grava certificado, chave e bundles de forma
atômica e só confirma o novo ID ao reconectar.

Troque o certificado servidor para a nova CA somente quando todos os Clusters
`Active` reportarem o `trustBundleId` alvo em `GET /api/v1/admin/clusters`. Remova
as raízes antigas somente depois dessa confirmação e do fim da sobreposição de
uma hora; Clusters offline além do prazo operacional devem ser revogados ou
recuperados explicitamente. O ID é derivado das raízes ativas e permanece igual
quando as raízes antigas são removidas do final dos bundles. Uma execução
idempotente de `molejoctl platform control-plane install` continua renovando apenas o
certificado servidor, preservando as raízes configuradas.

## Protocolo e reconciliação

O hello negocia versões do protocolo e capacidades, estabelece a sessão
autoritativa e informa a diferença de relógio. Comandos possuem versão do schema
do payload, versão desejada, fencing token persistido e deadline limitado pelo lease. O Agent
rejeita comandos incompatíveis, inválidos ou expirados. O control plane aceita o
resultado somente enquanto o lease e o fencing token no PostgreSQL forem
válidos; memória do processo não participa da correção.

Cada heartbeat pode incluir um snapshot completo das observações de
`AppDeployment` e `AppVolume` pertencentes à Molejo. O snapshot contém status,
versão desejada e SHA-256 canônico do `spec`, nunca configuração aberta nem
valores de Secrets. O control plane persiste as observações e reutiliza operações
duráveis quando um objeto não existe ou qualquer parte de seu `spec` diverge,
incluindo réplicas, recursos, portas, probes, exposição e volumes. O Platform
Operator continua responsável pela convergência Kubernetes de cada CR.
Heartbeats de sessões substituídas e sequências repetidas ou regressivas são
rejeitados antes de alterar o estado observado.

O posicionamento é explícito pelo vínculo Workspace-to-Cluster. Cada
AppEnvironment registra o Cluster de destino; adicionar um segundo Cluster não
move workloads existentes nem depende de um Agent global padrão.
Revogar um Cluster preserva workloads e histórico, marca seus vínculos como
`Failed` e seus AppEnvironments como `Unknown`. O mesmo UID Kubernetes pode ser
pareado novamente somente em um novo registro depois da revogação anterior.

## Saúde e recuperação

`/healthz` informa a saúde do processo. `/readyz` fica disponível em
`Unconfigured`, `Unpaired`, `Enrolling`, `Connecting` e `Paired`, e indisponível
em `Initializing`, `Stopping` ou `Failed`. `/status` expõe o estado sem material
de identidade. Falhas de rede retornam ao backoff limitado. Renovação interrompida
é repetida com a tentativa persistida; novo enrollment fica reservado a uma
identidade ausente, expirada ou revogada administrativamente.
