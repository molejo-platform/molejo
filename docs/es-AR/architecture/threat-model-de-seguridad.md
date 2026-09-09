# Threat model de seguridad

## Estado y alcance

Este es el threat model normativo del alfa para provisionamiento de Workspaces,
entrega de secrets y activación explícita de bindings. Cubre Console y
automatizaciones, control plane, PostgreSQL, providers y secret stores externos,
Cluster Agent outbound con mTLS, reconciler del límite, Platform Operator, API
Kubernetes y workload de aplicación.

Es un artefacto de diseño y verificación, no una afirmación de seguridad en
producción. Se revisa al cambiar actores, credenciales, límites de confianza,
endpoints, modos de entrega o privilegios del clúster.

## Objetivos

1. Capabilities y feature flags nunca conceden permisos.
2. Toda mutación valida la ancestry completa del producto.
3. Comandos remotos son tipados y no exponen APIs/manifests/selectors arbitrarios.
4. Cada credencial tiene el menor blast radius práctico.
5. Los valores son write-only y no aparecen en estado duradero o diagnósticos.
6. Reconciliación, rotación y cleanup son idempotentes ante replay o falla parcial.
7. Los controles fallan cerrados y producen auditoría sanitizada.
8. Discovery y observaciones del Agent nunca seleccionan ni activan bindings.

## Límites y flujo

```text
[Browser/automation] -- HTTPS --> [Control plane] -- metadata --> [PostgreSQL]
                                      |      |
                                      |      +--> [SecretValueStore externo]
                                      +-- mTLS outbound --> [Cluster Agent]
                                                               |
                                                               v
                                                        [API Kubernetes]
                                                          |          |
                                             [WorkspacePlacement]   [Secret]
                                                          |          |
                                               [Boundary reconciler] v
                                                 Namespace/RBAC [Operator] --> [App]
```

## Amenazas principales

| ID | STRIDE / OWASP | Riesgo | Controles y prueba |
|---|---|---|---|
| TM-01 | Spoofing / A07 | Sesión actúa en otro tenant. | CSRF, sesión, ancestry, auditoría; integración negativa. |
| TM-02 | Elevation / A01 | Availability se interpreta como permiso. | Capability, consentimiento, autorización y admission separados. |
| TM-03 | Elevation / A01/A02 | Agent/Operator alcanza todos namespaces. | RoleBindings por Workspace y SelfSubjectAccessReview negativo afuera. |
| TM-04 | Elevation / A01 | Input apunta Kubernetes arbitrario. | Comandos tipados, placement listo y rechazo de targets extraños. |
| TM-05 | Tampering / A08 | Replay o versión regresiva. | mTLS, session, sequence, deadline, idempotencia, lease y fencing. |
| TM-06 | Disclosure / A04 | Secret aparece en DB/API/log. | Store externo, handle opaco, write-only y sentinelas de redaction. |
| TM-07 | Disclosure / A01/A02 | Agent lista Secrets del clúster. | Sin list/watch; get/create/delete namespaced y namespaces separados. |
| TM-08 | Disclosure / A04 | etcd/node expone materialización. | Encryption at rest y hardening son evidencias del Cluster Operator. |
| TM-09 | Disclosure | App divulga el secret recibido. | Valor mínimo, scope por AppEnvironment, rotación; riesgo residual. |
| TM-10 | Tampering / A06 | CRD se convierte en PodSpec arbitrario. | Schema cerrado y rendering restringido. |
| TM-11 | DoS / A10 | Automatización crea recursos sin límite. | Idempotencia, rate, clases/clusters permitidos y quotas. |
| TM-12 | Repudiation / A09 | Acción privilegiada sin atribución. | Audit de actor, target, request, resultado y reason sanitizado. |
| TM-13 | Supply chain / A03/A08 | Imagen obtiene credencial privilegiada. | Digests, provenance, SBOM, scanning y ServiceAccounts separados. |
| TM-14 | SSRF / A01 | Endpoint de provider alcanza metadata interna. | Configuración solo del operador y endpoints tipados. |
| TM-15 | Exceptional / A10 | Falla parcial deja acceso huérfano. | Conditions, retry, ownership y pruebas de recuperación. |
| TM-16 | Tampering / A01/A08 | Discovery o evidencia comprometida activa un binding no deseado. | Binding tipado creado por flujo autenticado del Cluster Operator; evidencia del Agent no activa candidatos. |

## Estado actual y gaps

Ya están implementados y validados la decisión namespaced de provisionamiento,
`WorkspacePlacement`, credenciales separadas de runtime/discovery, entrega
inmutable just-in-time de secrets y sanitización. El Workspace legado de prueba
se elimina y recrea explícitamente, sin migración. Backend externo de secrets y
evidencia de encryption at rest siguen como capabilities opcionales no
configuradas.

## Invariantes y riesgos aceptados

- Feature Availability nunca autoriza.
- El consentimiento del Cluster Operator no se modifica desde el flujo normal.
- Entradas humanas y automáticas comparten caso de uso.
- Operator no recibe valores; Agent no lista Secrets.
- Namespace reduce impacto, pero no es hard multi-tenancy.
- Control plane ve plaintext en el modo alfa y una App puede divulgar lo recibido.
- Molejo no administra etcd, KMS, nodes, CNI o IAM cloud.
- Observaciones y candidatos descubiertos no activan bindings del Control Plane.

Cada cambio de actor, permiso, credencial, callback, command kind, CRD,
ServiceAccount, backend o delivery exige revisar este modelo y vincular pruebas a
los IDs afectados.

## Referencias

- [ADR 0019](../adr/0019-provisionamiento-de-workspace-y-limite-de-namespace.md)
- [ADR 0020](../adr/0020-custodia-de-secrets-y-entrega-al-runtime.md)
- [ADR 0021](../adr/0021-bindings-explicitos-gestionados-por-el-operador.md)
- [ADR 0022](../adr/0022-metricas-neutrales-con-consulta-prometheus.md)
- [OWASP Threat Modeling](https://cheatsheetseries.owasp.org/cheatsheets/Threat_Modeling_Cheat_Sheet.html)
- [OWASP Top 10: 2025](https://owasp.org/Top10/2025/0x00_2025-Introduction/)
- [Buenas prácticas RBAC Kubernetes](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
- [Buenas prácticas de Secrets Kubernetes](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
