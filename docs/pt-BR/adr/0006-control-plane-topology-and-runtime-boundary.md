# ADR-0006: Control Plane Topology and Runtime Boundary

Status: Draft

## Contexto

O primeiro beta da Molejo precisa de uma API de produto e um Console pequeno sem
transformar Kubernetes na API pública. PostgreSQL armazena intenção e histórico
enquanto o operator existente controla os filhos de runtime.

## Decisão

O control plane é um serviço Go de uma réplica, com Console React/Vite e
PostgreSQL. Ele aplica somente recursos `AppDeployment` por uma interface de
runtime declarada pelo consumidor. O operator continua sendo o único dono de
Deployments, Services e HTTPRoutes. Actors podem participar de vários Workspaces;
cada Workspace contém Projects, enquanto Apps e Environments são irmãos sob um
Project. Um AppDeployment vincula exatamente um App a um Environment desse mesmo
Project. Cada Workspace é materializado como Namespace gerenciado por uma
operação durável, com kubeconfig explícito ou credenciais in-cluster.

Esta é uma topologia pre-alpha sem HA. A API expõe recursos de produto
sanitizados e nunca retorna metadados ou objetos Kubernetes brutos.

## Consequências

A fronteira pode ser levada a um futuro management cluster, mas este incremento
não fornece descoberta de clusters remotos, HA ou recuperação de desastre de
produção.
