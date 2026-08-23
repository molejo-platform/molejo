# ADR-0003: Private Stateless Backend Runtime Contract

## Status

Draft

## Context

El primer corte ejecutable demostró que un `AppDeployment` puede administrar un
Deployment. Un backend stateless privado útil también necesita red interna
estable, recursos explícitos de runtime, semántica de salud y un perfil seguro de
container por defecto. Copiar tipos de Kubernetes a la API pública acoplaría el
contrato del producto a un sustrato de ejecución y expondría una superficie de
compatibilidad mucho mayor que la requerida en esta fase.

## Decision

`AppDeploymentSpec` expresará el runtime privado con campos propios del producto:
digest inmutable de imagen OCI, réplicas, puerto TCP, CPU en millicores, memoria en
MiB y paths HTTP de liveness y readiness. Requests y limits son obligatorios,
positivos, y los requests no pueden superar los limits. Los paths de probe
comienzan con `/` y tienen longitud limitada. Las quantities de Kubernetes se
producen solamente dentro del operator.

Cada `AppDeployment` posee exactamente un Deployment y un Service ClusterIP con
el mismo nombre y namespace. El container y el puerto se llaman `app` y `http`;
el Service apunta a ese puerto mediante un selector estable. El operator elimina
drift de exposición pública y preserva campos asignados por el API server. Observa
ambos hijos, recrea cualquiera que sea eliminado y no elimina hijos directamente.

El runtime se ejecuta como non-root, usa seccomp `RuntimeDefault`, no permite
privilege escalation ni capabilities y tiene root filesystem de solo lectura.
Startup usa el path de readiness con una ventana fija de 60 segundos; readiness
se ejecuta cada cinco segundos y liveness cada diez, ambos con timeout de dos
segundos y tres fallas. Estos valores son política de esta versión de la API, no
configuración del usuario.

Los campos de release observada avanzan solamente después de que ambos hijos
convergen. `Ready=True` exige un Service convergido y el rollout completo del
Deployment. El operator no lee Pods ni EndpointSlices; el status del Deployment
es la señal agregada del workload. Un conflicto en cualquiera de los hijos usa el
reason `OwnershipConflict` existente.

La prueba end-to-end construye dos versiones de una fixture HTTP local con
BuildKit, las carga en un cluster Kind descartable y las referencia por digest.
El egress público es una verificación manual separada y no hace que el gate
determinístico dependa de un servicio externo.

## Consequences

La API permanece independiente de los tipos Go de Kubernetes y tiene un mapeo
determinístico a un runtime privado seguro. La identidad del Service, la política
de probes, las unidades de recursos y la configuración de seguridad pasan a ser
comportamiento sensible a compatibilidad en `v1alpha1`.

El perfil fijo excluye intencionalmente el ajuste arbitrario de probes, variables
de entorno, volúmenes, autoscaling, perfiles alternativos de seguridad, exposición
pública y salud directa de endpoints. Los requisitos futuros deberán ampliar el
contrato del producto deliberadamente, sin exponer el PodSpec subyacente.

## Alternatives Considered

Exponer `corev1.ResourceRequirements`, probes y tipos de seguridad de container
en el CRD. Esta alternativa fue rechazada porque convertiría Kubernetes en la API
pública del producto.

Crear solamente un Deployment y permitir que los consumidores descubran Pods.
Esta alternativa fue rechazada porque la identidad de los Pods es efímera y no
provee un endpoint privado estable.

Leer Pods y EndpointSlices para calcular disponibilidad. Esta alternativa fue
rechazada porque el Deployment ya agrega el rollout y los permisos y watches
adicionales son innecesarios para este corte.

Publicar la imagen de la fixture en un registry remoto. Esta alternativa fue
rechazada porque las imágenes locales de BuildKit cargadas en Kind demuestran el
comportamiento sin credenciales de publicación ni mutación remota.

## References

- [Services de Kubernetes](https://kubernetes.io/docs/concepts/services-networking/service/)
- [Gestión de recursos en Kubernetes](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
- [Probes de Kubernetes](https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/)
- [Security context de Kubernetes](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/)
