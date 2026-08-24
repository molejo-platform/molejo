# ADR-0006: Control Plane Topology and Runtime Boundary

Status: Draft

## Contexto

El primer beta de Molejo necesita una API de producto y una consola pequeña sin
convertir Kubernetes en la API pública. PostgreSQL guarda intención e historial
mientras el operator existente controla los hijos de runtime.

## Decisión

El control plane es un servicio Go de una réplica, con consola React/Vite y
PostgreSQL. Solo aplica recursos `AppDeployment` mediante una interfaz de runtime
declarada por el consumidor. El operator sigue siendo el único dueño de
Deployments, Services y HTTPRoutes. La primera instalación usa un Namespace de
Workspace compartido y kubeconfig explícito o credenciales in-cluster.

Es una topología pre-alpha sin HA. La API expone recursos de producto
sanitizados y nunca devuelve metadatos ni objetos Kubernetes crudos.

## Consecuencias

La frontera puede migrar a un management cluster futuro, pero este incremento no
ofrece descubrimiento de clusters remotos, HA ni recuperación ante desastres de
producción.
