# ADR-0003: Private Stateless Backend Runtime Contract

## Status

Draft

## Context

O primeiro corte executável comprovou que um `AppDeployment` pode gerenciar um
Deployment. Um backend stateless privado útil também precisa de rede interna
estável, recursos explícitos de runtime, semântica de saúde e um perfil seguro de
container por padrão. Copiar tipos Kubernetes para a API pública acoplaria o
contrato do produto a um substrato de execução e exporia uma superfície de
compatibilidade muito maior do que esta fase exige.

## Decision

`AppDeploymentSpec` expressará o runtime privado com campos do domínio do produto:
digest imutável de imagem OCI, réplicas, porta TCP, CPU em millicores, memória em
MiB e paths HTTP de liveness e readiness. Requests e limits são obrigatórios,
positivos, e requests não podem superar limits. Paths de probe começam com `/` e
têm comprimento limitado. Quantities Kubernetes são produzidas somente dentro do
operator.

Cada `AppDeployment` possui exatamente um Deployment e um Service ClusterIP com o
mesmo nome e namespace. Container e porta se chamam `app` e `http`; o Service
aponta para essa porta nomeada por um seletor estável. O operator remove drift de
exposição pública e preserva campos alocados pelo API server. Ele observa os dois
filhos, recria qualquer filho removido e não exclui filhos diretamente.

O runtime executa como não root, usa seccomp `RuntimeDefault`, não permite
privilege escalation nem capabilities e possui root filesystem somente leitura.
Startup usa o path de readiness com janela fixa de 60 segundos; readiness executa
a cada cinco segundos e liveness a cada dez, ambos com timeout de dois segundos e
três falhas. Esses valores são política desta versão da API, não configuração do
usuário.

Os campos da release observada avançam somente depois da convergência dos dois
filhos. `Ready=True` exige Service convergido e rollout completo do Deployment. O
operator não lê Pods nem EndpointSlices; o status do Deployment é o sinal agregado
do workload. Conflito em qualquer filho usa o reason `OwnershipConflict` existente.

A prova end-to-end constrói duas versões de uma fixture HTTP local com BuildKit,
carrega ambas em um cluster Kind descartável e as referencia por digest. Egress
público é uma verificação manual separada e não faz o gate determinístico depender
de um serviço externo.

## Consequences

A API permanece independente dos tipos Go do Kubernetes e possui mapeamento
determinístico para um runtime privado seguro. Identidade do Service, política de
probes, unidades de recursos e configurações de segurança tornam-se comportamento
sensível à compatibilidade em `v1alpha1`.

O perfil fixo exclui intencionalmente ajuste arbitrário de probes, variáveis de
ambiente, volumes, autoscaling, perfis alternativos de segurança, exposição
pública e saúde direta de endpoints. Requisitos futuros devem ampliar o contrato
do produto deliberadamente, sem expor o PodSpec subjacente.

## Alternatives Considered

Expor `corev1.ResourceRequirements`, probes e tipos de segurança de container no
CRD. Essa alternativa foi rejeitada porque tornaria o Kubernetes a API pública do
produto.

Criar somente um Deployment e deixar consumidores descobrirem Pods. Essa
alternativa foi rejeitada porque a identidade de Pods é efêmera e não fornece um
endpoint privado estável.

Ler Pods e EndpointSlices para calcular prontidão. Essa alternativa foi rejeitada
porque o Deployment já agrega o rollout e permissões e watches adicionais são
desnecessários neste corte.

Publicar a imagem da fixture em um registry remoto. Essa alternativa foi rejeitada
porque imagens locais do BuildKit carregadas no Kind comprovam o comportamento sem
credenciais de publicação ou mutação remota.

## References

- [Services do Kubernetes](https://kubernetes.io/docs/concepts/services-networking/service/)
- [Gerenciamento de recursos no Kubernetes](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
- [Probes do Kubernetes](https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/)
- [Security context do Kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)
