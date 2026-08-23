# ADR-0006: Control Plane Topology and Runtime Boundary

Status: Draft

## Contexto

O primeiro beta da Fruto precisa de uma API de produto e um Console pequeno sem
transformar Kubernetes na API pública. PostgreSQL armazena intenção e histórico
enquanto o operator existente controla os filhos de runtime.

## Decisão

O control plane é um serviço Go de uma réplica, com Console React/Vite e
PostgreSQL. Ele aplica somente recursos `AppDeployment` por uma interface de
runtime declarada pelo consumidor. O operator continua sendo o único dono de
Deployments, Services e HTTPRoutes. A primeira instalação usa um Namespace de
Workspace compartilhado e kubeconfig explícito ou credenciais in-cluster.

Esta é uma topologia pre-alpha sem HA. A API expõe recursos de produto
sanitizados e nunca retorna metadados ou objetos Kubernetes brutos.

## Consequências

A fronteira pode ser levada a um futuro management cluster, mas este incremento
não fornece descoberta de clusters remotos, HA ou recuperação de desastre de
produção.
