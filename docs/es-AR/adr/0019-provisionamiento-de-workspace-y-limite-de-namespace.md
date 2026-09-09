# ADR 0019: provisionamiento de Workspace y límite de namespace

## Estado

Aceptado para la arquitectura alfa.

## Decisión

Cada solicitud de provisionamiento pasa por cuatro decisiones independientes:

1. el clúster reporta la capability técnica;
2. el Cluster Operator consiente el modo `Disabled` o `Namespaced` mediante la
   instalación del Agent;
3. el control plane autoriza al actor mediante un permiso atómico como
   `installation.workspace.create`;
4. una política pura admite clúster, namespace, clase, owner, límites e
   idempotencia.

Feature Availability describe como máximo los dos primeros puntos. Nunca concede
autorización ni reemplaza admission. En el alfa solo Installation Administrator
crea Workspaces. Un futuro Workspace Provisioner usa el mismo caso de uso
idempotente con restricciones explícitas y nunca llama al Agent directamente.

Un Workspace colocado en un clúster corresponde a un namespace de Molejo. Un CR
cluster-scoped `WorkspacePlacement` proyecta solamente identidad inmutable,
namespace, perfil fijo de acceso y ciclo de vida. No acepta manifests, verbos
RBAC, role refs, ServiceAccounts, selectors ni configuración arbitraria.

El Agent reconcilia `WorkspacePlacement`, pero no crea Namespace o RBAC. Un
reconciler de límites aislado crea namespaces con ownership compatible y
RoleBindings para roles fijos. El provisionamiento del límite y la reconciliación
de aplicaciones usan workloads y ServiceAccounts separados, aunque puedan
compartir el mismo artefacto binario.

## Consecuencias

La configuración del clúster no concede privilegios de producto, el blast radius
del Agent queda limitado a namespaces vinculados y la creación pasa a ser
asíncrona. El reconciler de límites sigue siendo un componente de alta confianza.
Namespaces reducen impacto, pero no ofrecen aislamiento fuerte contra workloads
hostiles en el mismo clúster o node.

## Alternativas consideradas

Bindings cluster-wide, RBAC dinámico directo en el Agent, una feature flag
autoritativa en el cliente y la creación manual obligatoria con `molejoctl`
fueron rechazados por ampliar privilegios o impedir self-service.

## Referencias

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [ADR 0018: observación de capacidades y disponibilidad](0018-observacion-de-capacidades-y-disponibilidad-de-features.md)
- [Threat model de seguridad](../architecture/threat-model-de-seguridad.md)
- [Buenas prácticas RBAC de Kubernetes](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
