# Security threat model

## Status and scope

This is the normative alpha threat model for Workspace provisioning,
application-secret delivery, and explicit capability-binding activation. It
covers the Console and external automation, Control Plane, PostgreSQL, external
providers and secret stores, outbound mTLS Cluster Agent, Workspace boundary
reconciliation, Platform Operator, Kubernetes API, and application workload.

It is a design and verification artifact, not a production-security claim. It
must be reviewed whenever a trust boundary, actor, credential, public endpoint,
secret delivery mode, or cluster privilege changes.

## Security objectives

1. A cluster fact or client-side feature flag never grants product permission.
2. Every resource mutation is authorized against its complete product ancestry.
3. Remote commands address typed Molejo resources, never arbitrary Kubernetes
   APIs, manifests, selectors, or provider queries.
4. A compromised component has the smallest practical cluster and product blast
   radius.
5. Secret values are write-only at the public API and absent from durable
   Control Plane state, diagnostics, and observations.
6. Reconciliation, rotation, retries, and cleanup remain idempotent under
   disconnects, replay, takeover, and partial failure.
7. Security controls fail closed and produce sanitized, attributable audit
   evidence.
8. Discovery and Agent observations never select or activate provider or cluster
   capability bindings.

## Assets

- human sessions, CSRF tokens, invitation and recovery credentials;
- automation tokens and future federated workload identities;
- Agent bootstrap tokens, private keys, certificates, and trust bundles;
- Workspace identity, membership, roles, access grants, placement, and quotas;
- application secret values, opaque handles, versions, and fingerprints;
- desired application state and immutable release identity;
- Kubernetes ServiceAccount tokens, Namespaces, RoleBindings, and workloads;
- provider credentials and cluster administration credentials;
- audit history and operation sequence/fencing state.

## Actors and trust levels

| Actor | Trust and authority |
|---|---|
| Cluster Operator | Controls cluster installation, Agent configuration, and cluster policy. It is outside normal product authorization. |
| Installation Administrator | Human Control Plane actor allowed to govern the installation and create Workspaces in the alpha. |
| Workspace Provisioner | Future non-human actor constrained to the shared Workspace creation use case. |
| Workspace Owner/Member/Viewer | Product actors constrained by Workspace ancestry and atomic permissions. |
| Control Plane | Product source of truth, authorization point, operation coordinator, and alpha secret custodian. |
| Cluster Agent | Authenticated executor and observer for one registered cluster; it is not a policy authority. |
| Workspace boundary reconciler | High-trust, cluster-scoped bootstrap authority with no secret or application-runtime responsibility. |
| Platform Operator | Reconciles closed Molejo CRDs and their children in ready Workspace namespaces. |
| Application workload | Untrusted application code that may access only the values intentionally delivered to it. |
| External provider | Independently operated system satisfying one typed capability contract. |

## Trust boundaries and flows

```text
[Browser or automation]
          |
          | HTTPS + actor authentication
          v
[Control Plane] ---- [PostgreSQL metadata and audit]
      |   |
      |   +---- TLS/workload identity ---- [External SecretValueStore]
      |
      +---- outbound-established mTLS ---- [Cluster Agent]
                                                |
                                                | bounded, versioned desired state
                                                v
                                        [Kubernetes API]
                                           |          |
                              [WorkspacePlacement]   [versioned Secret]
                                           |          |
                               [Boundary reconciler]  |
                                           |          v
                                 Namespace/RBAC   [Platform Operator]
                                                      |
                                                      v
                                               [Application Pod]
```

The Control Plane resolves product targets before dispatch. The Agent validates
cluster identity, Workspace ownership, namespace ownership, runtime ownership,
session, sequence, deadline, and payload bounds before touching Kubernetes. The
Platform Operator accepts a closed product schema and cannot receive provider
credentials or secret values.

## Threat register

| ID | STRIDE / OWASP | Scenario and impact | Required controls | Verification |
|---|---|---|---|---|
| TM-01 | Spoofing / A07 | Stolen human session creates or changes another tenant's resources. | Secure cookie, CSRF on mutation, session invalidation, complete ancestry authorization, audit actor. | Negative HTTP integration across two Workspaces. |
| TM-02 | Elevation / A01 | Availability reported by an Agent is treated as permission to create a Workspace. | Capability, cluster consent, actor authorization, and admission are separate decisions; API rechecks all server-side gates. | Pure decision matrix and direct API denial despite available capability. |
| TM-03 | Elevation / A01/A02 | Stolen Agent or Operator token accesses every namespace through a ClusterRoleBinding. | Per-Workspace RoleBindings; retain cluster-wide grants only for inherently cluster-scoped reads and fixed Molejo CRs. | ServiceAccount SelfSubjectAccessReview positive inside and negative outside owned namespaces. |
| TM-04 | Elevation / A01 | Remote input asks the Agent to mutate arbitrary Kubernetes resources or target a foreign namespace. | Closed versioned commands, no raw YAML/GVR/selectors, placement readiness, immutable ownership identifiers, reserved namespace rejection. | Unit and envtest foreign-target cases. |
| TM-05 | Tampering / A08 | Command replay, stale worker, or Agent takeover applies an older desired version. | mTLS identity, session ID, monotonic sequence, deadline, idempotency key, desired version, lease, and fencing token. | Real TLS in-memory replay, regression, takeover, and reconnect tests. |
| TM-06 | Information disclosure / A04 | Secret is stored in PostgreSQL, returned by an API, or emitted in logs/errors. | External value store, opaque handle, write-only API, structured redaction, sanitized errors, no value snapshots. | Parameter integration tests and log/snapshot sentinels. |
| TM-07 | Information disclosure / A01/A02 | Agent lists Secrets cluster-wide or reads provider credentials in a shared namespace. | No Secret list/watch; namespaced get/create/delete only; separate system/capability namespaces. | RBAC contract tests and live negative access checks. |
| TM-08 | Information disclosure / A04 | etcd or a node exposes materialized Kubernetes Secrets. | Cluster encryption at rest, protected etcd access, node hardening, short-lived/versioned values, bounded retention. | `doctor` warning plus operator-owned cluster evidence; never claim Molejo enforcement. |
| TM-09 | Information disclosure | Application exfiltrates a secret intentionally delivered to it. | Secret scoped per application environment, minimal values, rotation/revocation, network policy when configured, no Kubernetes token by default. | Render tests and workload security-context acceptance. Residual risk is accepted. |
| TM-10 | Tampering / A06 | A user-controlled AppDeployment field becomes an arbitrary PodSpec escape. | Closed CRD schema, fixed ServiceAccount behavior, restricted pod security context, no arbitrary volumes, Secret refs, host settings, or init containers. | Rendering and schema rejection tests. |
| TM-11 | Denial of service / A10 | Workspace automation creates unbounded namespaces or operations. | Idempotency, rate limits, allowed clusters/classes, quotas, operation concurrency, bounded retries, auditable denial. | Duplicate-event and quota/rate tests before enabling automation actors. |
| TM-12 | Repudiation / A09 | A privileged operation cannot be attributed or logs contain only infrastructure identity. | Audit human/machine actor, permission, target ancestry, cluster, request/idempotency ID, outcome, and reason without sensitive values. | Audit integration tests for accepted and denied actions. |
| TM-13 | Supply chain / A03/A08 | Compromised image or chart gains an Agent, Operator, or boundary-controller token. | Immutable digests, provenance/signature verification, minimal images, SBOM, dependency scanning, separated ServiceAccounts. | Release and installation conformance gates. |
| TM-14 | SSRF / A01 | Provider endpoint or webhook target supplied by a Workspace actor reaches metadata or internal services. | Provider endpoints are installation/operator configuration, typed allowlisted schemes/hosts, no arbitrary URL in runtime commands. | Adapter validation and blocked private/metadata target tests. |
| TM-15 | Exceptional condition / A10 | Partial provisioning leaves a namespace privileged, orphaned, or usable before policy is ready. | Condition-based state machine, access granted only after ownership checks, retry-safe plans, finalizers/explicit deletion policy, no success before all required conditions. | Envtest interruption and recovery matrix. |
| TM-16 | Tampering / A01/A08 | Compromised discovery or Agent evidence activates a malicious or unintended storage, publication, or telemetry binding. | Typed Control Plane binding owned by an authenticated Cluster Operator workflow; Agent evidence is read-only and cannot activate candidates. | API authorization/admission tests and a negative observation-to-binding mutation test. |

## Current implementation evidence and gaps

Implemented controls include human session and CSRF checks, installation and
Workspace authorization, operation idempotency, Agent mTLS identity, session and
sequence validation, desired-version fencing, immutable versioned configuration,
ownership checks, closed application workload rendering, and disabled automatic
ServiceAccount token mounting for application Pods.

The namespaced provisioning decision, `WorkspacePlacement` boundary controller,
namespaced runtime and observation permissions, immutable just-in-time secret
delivery, and sanitized transport/persistence boundaries are implemented. The
current Agent workload uses one ServiceAccount for runtime, observation, and
discovery; those permissions are separate roles but not separate credentials.

An external secret backend and operator-owned Kubernetes encryption-at-rest
evidence are not configured in the current K3s installation. They remain
optional capability state and do not make the core application loop unhealthy.

## Security invariants

- Feature Availability never contains actor permission and never authorizes a
  mutation.
- Cluster Operator consent cannot be changed by a normal Control Plane actor.
- Workspace creation has one application use case regardless of human or
  automation entry point.
- Cluster Agent and Platform Operator never accept a user-supplied PodSpec,
  RoleBinding, ServiceAccount, namespace selector, or Kubernetes API path.
- A Workspace is not ready until namespace ownership and both namespaced access
  bindings are observed.
- Secret values never appear in read APIs or durable Control Plane storage.
- Platform Operator remains secret-blind; Cluster Agent Secret access is
  namespaced and excludes list/watch.
- Optional provider failure cannot make the core Control Plane unhealthy or
  silently change desired application state.
- Agent observations and discovered candidates cannot create or activate a
  Control Plane binding.
- A cluster administrator can always override Molejo controls; this actor is
  outside Molejo tenant isolation.

## HTTP publication foundation (TM-04, TM-05, TM-07, TM-16)

The [publication contract](http-publication.md) rejects legacy or partial runtime
payloads before effects, fixes a per-address Gateway/listener target, and preserves
foreign resource ownership. Only namespaces explicitly labeled
`platform.molejo.dev/http-publication=enabled` attach to the managed Gateway recipe;
Molejo owns this label on Workspace and control-plane namespaces. The label is an
infrastructure attachment boundary, not a product domain grant. Workloads must
not be allowed to mutate namespaces, AppDeployments or HTTPRoutes directly.
External Gateway inspection requires neither Helm ownership nor Secret reads.

CP result fencing and a missing Kubernetes object alone do not prove that a late
Apply cannot recreate a deleted route. Phase-two operation/claim transactions must
establish quiescence before final correlated withdrawal and claim release; until
then the old HTTP configuration is rejected before command dispatch. Foreground
deletion, update preconditions and live desired-version checks are complementary
runtime controls, not a durable post-deletion fence.

## Accepted residual risks and non-goals

- Alpha releases may require a clean reinstall and do not promise in-place
  authorization/RBAC migration compatibility.
- Namespace isolation is not hard multi-tenancy against hostile workloads,
  container escapes, kernel compromise, or a malicious cluster administrator.
- The Control Plane sees plaintext during alpha materialization and is therefore
  in the secret custody boundary.
- An application can disclose a secret intentionally delivered to it.
- Molejo does not configure etcd, KMS, nodes, CNI, cloud IAM, or cluster network
  policy automatically as part of the application loop.

## Review and validation

The model follows the OWASP questions: what is being built, what can go wrong,
what will be done, and whether the controls are proven. Each implementation cut
must link changed trust boundaries to one or more threat IDs and add the lowest
cost test that can detect regression.

Review is mandatory when adding an actor, permission, provider credential,
public callback, command kind, CRD field, ServiceAccount permission, secret
backend, delivery mode, or cluster-scoped controller.

## References

- [ADR-0001: Product boundary and runtime topology](../adr/0001-product-boundary-and-runtime-topology.md)
- [ADR-0003: Principals, authentication, and authorization](../adr/0003-principals-authentication-and-authorization.md)
- [ADR-0004: Workspace placement and Kubernetes privilege boundary](../adr/0004-workspace-placement-and-kubernetes-privilege-boundary.md)
- [ADR-0005: Capability composition and explicit bindings](../adr/0005-capability-composition-and-explicit-bindings.md)
- [ADR-0006: Secret custody and runtime delivery](../adr/0006-secret-custody-and-runtime-delivery.md)
- [OWASP Threat Modeling Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Threat_Modeling_Cheat_Sheet.html)
- [OWASP Top 10: 2025](https://owasp.org/Top10/2025/0x00_2025-Introduction/)
- [Kubernetes RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
- [Good practices for Kubernetes Secrets](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
- [Kubernetes Service Accounts](https://kubernetes.io/docs/concepts/security/service-accounts/)
