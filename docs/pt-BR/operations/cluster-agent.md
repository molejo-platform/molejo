# Operação do Cluster Agent

O Cluster Agent é o único componente da Molejo que executa a intenção de runtime
do control plane no Kubernetes. Ele inicia um stream outbound com mTLS e TLS 1.3;
a API pública não recebe kubeconfig e o cluster não expõe porta de gerenciamento
de entrada.

## Instalação e confiança

Instale Operator e Agent com `molejoctl cluster install` e execute
`molejoctl control-plane install`. Uma instalação nova cria raízes ECDSA P-256
separadas:

- `molejo-agent-ca` assina identidades cliente dos Agents;
- `molejo-control-plane-server-ca` assina a identidade interna da API e do gRPC.

A separação impede que uma chave de servidor comprometida emita identidades de
Agent. Instalações alpha antigas que ainda usam uma única CA mantêm esse arranjo
até o upgrade do chart; uma nova execução idempotente do instalador não interrompe
o Console nem a conexão do Agent.

O ServiceAccount do Agent possui acesso restrito aos dois Secrets nomeados de
identidade e aos recursos Kubernetes exigidos pelo contrato de runtime. Ele não
lista Secrets arbitrários. Secrets de configuração são selecionados e geridos
somente por objetos pertencentes à Molejo.

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
retorna a mesma credencial. Revogar um Cluster invalida todas as credenciais e
falha operações enfileiradas ou arrendadas para ele.

A substituição da raiz de confiança não é um reparo automático. Mantenha backup
dos dois Secrets de CA. Se a CA de servidor for perdida, o instalador interrompe
e exige restauração em vez de trocar a confiança silenciosamente e desconectar
Agents já pareados.
Uma execução idempotente de `molejoctl control-plane install` renova o certificado
de servidor quando restarem menos de 30 dias, preservando a mesma CA confiável.

## Protocolo e reconciliação

O hello negocia versões do protocolo e capacidades. Comandos possuem versão do
schema do payload, versão desejada, fencing token persistido e deadline. O Agent
rejeita comandos incompatíveis, inválidos ou expirados. O control plane aceita o
resultado somente enquanto o lease e o fencing token no PostgreSQL forem
válidos; memória do processo não participa da correção.

Cada heartbeat pode incluir um snapshot completo das observações de
`AppDeployment` e `AppVolume` pertencentes à Molejo. O snapshot contém status e
identificadores, nunca configurações nem valores de Secrets. O control plane
persiste as observações e reutiliza a operação durável `ApplyDeployment` quando
o objeto não existe ou a imagem observada diverge do desejado. O Platform
Operator continua responsável pela convergência Kubernetes de cada CR.

O posicionamento é explícito pelo vínculo Workspace-to-Cluster. Cada
AppEnvironment registra o Cluster de destino; adicionar um segundo Cluster não
move workloads existentes nem depende de um Agent global padrão.

## Saúde e recuperação

`/healthz` informa a saúde do processo. `/readyz` fica disponível em
`Unconfigured`, `Unpaired`, `Enrolling`, `Connecting` e `Paired`, e indisponível
em `Initializing`, `Stopping` ou `Failed`. `/status` expõe o estado sem material
de identidade. Falhas de rede retornam ao backoff limitado. Renovação interrompida
é repetida com a tentativa persistida; novo enrollment fica reservado a uma
identidade ausente, expirada ou revogada administrativamente.
