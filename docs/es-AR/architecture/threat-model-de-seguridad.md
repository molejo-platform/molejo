# Threat model de seguridad

## Estado y alcance

Este es el threat model normativo del alfa para el aprovisionamiento de
Workspaces, la entrega de secrets de aplicaciones y la activación de Bindings
explícitos de Capabilities. Cubre Console y automatizaciones externas, Control
Plane, PostgreSQL, Providers y almacenes de secrets externos, el Cluster Agent
saliente con mTLS, la reconciliación de límites de Workspace, Platform Operator,
la API de Kubernetes y el workload de la aplicación.

Es un artefacto de diseño y verificación, no una afirmación de seguridad en
producción. Debe revisarse cuando cambie un límite de confianza, actor,
credencial, endpoint público, modo de entrega de secrets o privilegio del clúster.

## Objetivos de seguridad

1. Un hecho del clúster o una feature flag del cliente nunca concede un permiso
   del producto.
2. Toda mutación de recursos se autoriza contra su ascendencia completa en el
   producto.
3. Los comandos remotos se dirigen a recursos Molejo tipados, nunca a APIs,
   manifiestos, selectores ni consultas de Providers arbitrarios de Kubernetes.
4. Un componente comprometido tiene el menor blast radius práctico en el clúster
   y el producto.
5. Los valores de secrets son write-only en la API pública y no aparecen en el
   estado durable, los diagnósticos ni las observaciones del Control Plane.
6. La reconciliación, rotación, reintentos y limpieza permanecen idempotentes ante
   desconexiones, replay, takeover y fallas parciales.
7. Los controles de seguridad fallan de forma cerrada y producen evidencia de
   auditoría sanitizada y atribuible.
8. El descubrimiento y las observaciones del Agent nunca seleccionan ni activan
   Bindings de Capabilities de Provider o clúster.

## Activos protegidos

- sesiones humanas, tokens CSRF y credenciales de invitación y recuperación;
- tokens de automatización y futuras identidades federadas de workloads;
- tokens de bootstrap, claves privadas, certificados y bundles de confianza del Agent;
- identidad, membresía, roles, accesos, placement y cuotas del Workspace;
- valores, handles opacos, versiones y fingerprints de secrets de aplicaciones;
- estado deseado de aplicaciones e identidad inmutable de Releases;
- tokens de ServiceAccounts, Namespaces, RoleBindings y workloads de Kubernetes;
- credenciales de Providers y de administración del clúster;
- historial de auditoría y estado de secuencia y fencing de operaciones.

## Actores y niveles de confianza

| Actor | Confianza y autoridad |
| --- | --- |
| Cluster Operator | Controla la instalación del clúster, la configuración del Agent y las políticas del clúster. Está fuera de la autorización normal del producto. |
| Installation Administrator | Actor humano del Control Plane autorizado a gobernar la instalación y crear Workspaces durante el alfa. |
| Workspace Provisioner | Futuro actor no humano limitado al caso de uso compartido de creación de Workspaces. |
| Workspace Owner/Member/Viewer | Actores del producto limitados por la ascendencia del Workspace y permisos atómicos. |
| Control Plane | Fuente de verdad del producto, punto de autorización, coordinador de operaciones y custodio de secrets durante el alfa. |
| Cluster Agent | Ejecutor y observador autenticado de un clúster registrado; no es una autoridad de políticas. |
| Reconciliador de límites de Workspace | Autoridad de bootstrap de alta confianza y con alcance de clúster, sin responsabilidad sobre secrets ni runtime de aplicaciones. |
| Platform Operator | Reconcilia CRDs cerrados de Molejo y sus recursos hijos en Namespaces de Workspace listos. |
| Workload de aplicación | Código de aplicación no confiable que solo puede acceder a valores entregados intencionalmente. |
| Provider externo | Sistema operado de forma independiente que satisface un contrato tipado de Capability. |

## Límites de confianza y flujos

```text
[Browser o automatización]
          |
          | HTTPS + autenticación del actor
          v
[Control Plane] ---- [Metadatos y auditoría en PostgreSQL]
      |   |
      |   +---- TLS/identidad de workload ---- [SecretValueStore externo]
      |
      +---- mTLS establecido de forma saliente ---- [Cluster Agent]
                                                        |
                                                        | estado deseado limitado y versionado
                                                        v
                                                [API de Kubernetes]
                                                   |          |
                                      [WorkspacePlacement]   [Secret versionado]
                                                   |          |
                                       [Reconciliador]        |
                                                   |          v
                                         Namespace/RBAC  [Platform Operator]
                                                              |
                                                              v
                                                       [Pod de aplicación]
```

El Control Plane resuelve los objetivos del producto antes de enviar comandos.
El Agent valida identidad del clúster, ownership del Workspace, ownership del
Namespace y del runtime, sesión, secuencia, deadline y límites del payload antes
de tocar Kubernetes. El Platform Operator acepta un schema cerrado del producto
y no puede recibir credenciales de Providers ni valores de secrets.

## Registro de amenazas

| ID | STRIDE / OWASP | Escenario e impacto | Controles requeridos | Verificación |
| --- | --- | --- | --- | --- |
| TM-01 | Spoofing / A07 | Una sesión humana robada crea o modifica recursos de otro tenant. | Cookie segura, CSRF en mutaciones, invalidación de sesión, autorización de ascendencia completa y auditoría del actor. | Integración HTTP negativa entre dos Workspaces. |
| TM-02 | Elevation / A01 | La disponibilidad informada por un Agent se interpreta como permiso para crear un Workspace. | Capability, consentimiento del clúster, autorización del actor y admission son decisiones separadas; la API vuelve a comprobar todos los gates en el servidor. | Matriz de decisión pura y denegación directa de la API aunque la Capability esté disponible. |
| TM-03 | Elevation / A01/A02 | Un token robado del Agent u Operator accede a todos los Namespaces mediante ClusterRoleBinding. | RoleBindings por Workspace; conservar permisos globales solo para lecturas inherentemente cluster-scoped y CRs fijos de Molejo. | SelfSubjectAccessReview positivo dentro y negativo fuera de los Namespaces administrados. |
| TM-04 | Elevation / A01 | Una entrada remota solicita al Agent modificar recursos Kubernetes arbitrarios o apuntar a un Namespace ajeno. | Comandos cerrados y versionados, sin YAML/GVR/selectores crudos, readiness del placement, identificadores de ownership inmutables y rechazo de Namespaces reservados. | Casos unitarios y envtest con objetivos ajenos. |
| TM-05 | Tampering / A08 | Replay de comandos, worker obsoleto o takeover del Agent aplica una versión deseada anterior. | Identidad mTLS, session ID, secuencia monotónica, deadline, idempotency key, versión deseada, lease y fencing token. | Pruebas en memoria con TLS real para replay, regresión, takeover y reconexión. |
| TM-06 | Information disclosure / A04 | Un secret se almacena en PostgreSQL, se devuelve por una API o aparece en logs o errores. | Almacén externo, handle opaco, API write-only, redaction estructurada, errores sanitizados y ausencia de snapshots de valores. | Pruebas de integración de parámetros y sentinels en logs y snapshots. |
| TM-07 | Information disclosure / A01/A02 | El Agent lista Secrets globalmente o lee credenciales de Providers en un Namespace compartido. | Sin list/watch de Secrets; solo get/create/delete con alcance de Namespace; Namespaces separados para sistema y Capabilities. | Pruebas de contrato RBAC y verificaciones negativas en vivo. |
| TM-08 | Information disclosure / A04 | etcd o un node expone Secrets materializados de Kubernetes. | Cifrado en reposo del clúster, acceso protegido a etcd, hardening de nodes, valores de corta duración y versionados, retención limitada. | Advertencia de `doctor` y evidencia perteneciente al operador del clúster; nunca afirmar que Molejo lo aplica. |
| TM-09 | Information disclosure | La aplicación exfiltra un secret entregado intencionalmente. | Secret con alcance por AppEnvironment, valores mínimos, rotación y revocación, network policy cuando esté configurada y sin token Kubernetes por defecto. | Pruebas de render y aceptación del security context del workload. El riesgo residual se acepta. |
| TM-10 | Tampering / A06 | Un campo de AppDeployment controlado por el usuario se convierte en escape arbitrario de PodSpec. | Schema CRD cerrado, comportamiento fijo del ServiceAccount, security context restringido, sin volúmenes, Secret refs, configuración de host ni init containers arbitrarios. | Pruebas de render y rechazo por schema. |
| TM-11 | Denial of service / A10 | Una automatización de Workspace crea Namespaces u operaciones sin límite. | Idempotencia, rate limits, Clusters y clases permitidos, cuotas, concurrencia de operaciones, reintentos limitados y denegación auditable. | Pruebas de eventos duplicados, cuotas y rate limits antes de habilitar actores de automatización. |
| TM-12 | Repudiation / A09 | Una operación privilegiada no puede atribuirse o los logs solo contienen una identidad de infraestructura. | Auditar actor humano o máquina, permiso, ascendencia objetivo, Cluster, request/idempotency ID, resultado y razón sin valores sensibles. | Pruebas de integración de auditoría para acciones aceptadas y denegadas. |
| TM-13 | Supply chain / A03/A08 | Una imagen o chart comprometido obtiene un token del Agent, Operator o reconciliador de límites. | Digests inmutables, verificación de procedencia y firma, imágenes mínimas, SBOM, análisis de dependencias y ServiceAccounts separados. | Gates de conformidad de release e instalación. |
| TM-14 | SSRF / A01 | Un endpoint de Provider o webhook suministrado por un actor del Workspace alcanza metadata o servicios internos. | Los endpoints de Providers son configuración de la instalación o del operador, con esquemas y hosts tipados y permitidos, sin URL arbitraria en comandos de runtime. | Validación del adapter y pruebas de bloqueo de metadata y destinos privados. |
| TM-15 | Exceptional condition / A10 | Un aprovisionamiento parcial deja un Namespace privilegiado, huérfano o utilizable antes de que la política esté lista. | Máquina de estados basada en Conditions, acceso concedido solo después de verificar ownership, planes seguros para reintentos, finalizers o política explícita de eliminación y ausencia de éxito antes de cumplir todas las condiciones. | Matriz envtest de interrupción y recuperación. |
| TM-16 | Tampering / A01/A08 | Un descubrimiento o evidencia del Agent comprometido activa un Binding malicioso o no deseado de storage, publicación o telemetría. | Binding tipado del Control Plane perteneciente a un flujo autenticado del Cluster Operator; la evidencia del Agent es read-only y no puede activar candidatos. | Pruebas de autorización y admission de la API, y prueba negativa de mutación desde observación hacia Binding. |

## Evidencia actual de implementación y gaps

Los controles implementados incluyen sesión humana y CSRF, autorización de
instalación y Workspace, idempotencia de operaciones, identidad mTLS del Agent,
validación de sesión y secuencia, fencing de versión deseada, configuración
inmutable y versionada, verificaciones de ownership, render cerrado de workloads
de aplicación y desactivación del montaje automático del token de ServiceAccount
en los Pods de aplicaciones.

Están implementados la decisión de aprovisionamiento namespaced, el controller de
límites de `WorkspacePlacement`, los permisos namespaced de runtime y observación,
la entrega inmutable just-in-time de secrets y los límites sanitizados de
transporte y persistencia. El workload actual del Agent usa un ServiceAccount para
runtime, observación y discovery; esos permisos están en roles separados, pero no
usan credenciales separadas.

En la instalación K3s actual no están configurados un backend externo de secrets
ni evidencia, bajo control del operador, de cifrado en reposo de Kubernetes.
Permanecen como estado opcional de Capabilities y no vuelven unhealthy al ciclo
principal de aplicaciones.

## Invariantes de seguridad

- Feature Availability nunca contiene permisos de actores ni autoriza una mutación.
- El consentimiento del Cluster Operator no puede ser modificado por un actor normal del Control Plane.
- La creación de Workspace tiene un único caso de uso de aplicación, sin importar si la entrada es humana o automatizada.
- Cluster Agent y Platform Operator nunca aceptan PodSpec, RoleBinding, ServiceAccount, selector de Namespace ni ruta de API Kubernetes suministrados por el usuario.
- Un Workspace no está listo hasta que se observen el ownership del Namespace y ambos Bindings de acceso namespaced.
- Los valores de secrets nunca aparecen en APIs de lectura ni en el estado durable del Control Plane.
- El Platform Operator permanece ciego a secrets; el acceso a Secrets del Cluster Agent tiene alcance de Namespace y excluye list/watch.
- La falla de un Provider opcional no puede volver unhealthy al Control Plane principal ni cambiar silenciosamente el estado deseado de aplicaciones.
- Las observaciones del Agent y los candidatos descubiertos no pueden crear ni activar un Binding del Control Plane.
- Un administrador del clúster siempre puede sobrepasar los controles de Molejo; este actor está fuera del aislamiento entre tenants de Molejo.

## Riesgos residuales aceptados y no objetivos

- Las releases alfa pueden requerir una reinstalación limpia y no prometen compatibilidad de migraciones in-place de autorización o RBAC.
- El aislamiento por Namespace no es hard multi-tenancy contra workloads hostiles, escapes de containers, compromiso del kernel ni administradores maliciosos del clúster.
- El Control Plane ve el texto plano durante la materialización alfa y, por lo tanto, forma parte del límite de custodia de secrets.
- Una aplicación puede divulgar un secret que le fue entregado intencionalmente.
- Molejo no configura automáticamente etcd, KMS, nodes, CNI, IAM del cloud ni network policy del clúster como parte del ciclo de aplicaciones.

## Revisión y validación

El modelo sigue las preguntas de OWASP: qué se está construyendo, qué puede salir
mal, qué se hará y si los controles están probados. Cada corte de implementación
debe vincular los límites de confianza modificados con uno o más IDs de amenazas y
agregar la prueba de menor costo capaz de detectar una regresión.

La revisión es obligatoria al agregar un actor, permiso, credencial de Provider,
callback público, tipo de comando, campo CRD, permiso de ServiceAccount, backend
de secrets, modo de entrega o controller con alcance de clúster.

## Referencias

- [ADR-0001: Límite del producto y topología de runtime](../../en/adr/0001-product-boundary-and-runtime-topology.md)
- [ADR-0003: Principals, autenticación y autorización](../../en/adr/0003-principals-authentication-and-authorization.md)
- [ADR-0004: Placement de Workspace y límite de privilegios Kubernetes](../../en/adr/0004-workspace-placement-and-kubernetes-privilege-boundary.md)
- [ADR-0005: Composición de Capabilities y Bindings explícitos](../../en/adr/0005-capability-composition-and-explicit-bindings.md)
- [ADR-0006: Custodia y entrega de secrets](../../en/adr/0006-secret-custody-and-runtime-delivery.md)
- [OWASP Threat Modeling Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Threat_Modeling_Cheat_Sheet.html)
- [OWASP Top 10: 2025](https://owasp.org/Top10/2025/0x00_2025-Introduction/)
- [Buenas prácticas de RBAC en Kubernetes](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
- [Buenas prácticas para Secrets de Kubernetes](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
- [Service Accounts de Kubernetes](https://kubernetes.io/docs/concepts/security/service-accounts/)
