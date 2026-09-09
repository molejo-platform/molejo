# Threat model de segurança

## Status e escopo

Este é o threat model normativo do alpha para provisionamento de Workspaces,
entrega de secrets de aplicações e ativação explícita de bindings de
capabilities. Ele cobre Console e automações externas, control plane, PostgreSQL,
providers e secret stores externos, Cluster Agent outbound com mTLS,
reconciliação do limite do Workspace, Platform Operator, API Kubernetes e
workload da aplicação.

É um artefato de design e verificação, não uma alegação de segurança em produção.
Deve ser revisto quando mudar um limite de confiança, ator, credencial, endpoint
público, modo de entrega de secrets ou privilégio no cluster.

## Objetivos de segurança

1. Fatos do cluster e feature flags client-side nunca concedem permissões.
2. Mutações validam autorização sobre toda a ancestry do produto.
3. Comandos remotos endereçam recursos tipados da Molejo, nunca APIs Kubernetes,
   manifests, selectors ou queries arbitrárias.
4. Componentes comprometidos possuem o menor blast radius prático.
5. Valores de secrets são write-only na API e ausentes de estado durável,
   diagnósticos e observações.
6. Reconciliação, rotação, retry e cleanup permanecem idempotentes sob
   desconexão, replay, takeover e falha parcial.
7. Controles falham de forma fechada e produzem auditoria sanitizada e atribuível.
8. Discovery e observações do Agent nunca selecionam nem ativam bindings de
   providers ou capabilities do cluster.

## Ativos protegidos

- sessões humanas, CSRF, convites e credenciais de recuperação;
- tokens de automação e futuras identidades federadas;
- tokens de bootstrap, chaves, certificados e trust bundles do Agent;
- identidade, membros, roles, grants, placement e quotas do Workspace;
- valores, handles opacos, versões e fingerprints de secrets;
- estado desejado e identidade imutável de releases;
- ServiceAccount tokens, Namespaces, RoleBindings e workloads Kubernetes;
- credenciais de providers e administração do cluster;
- histórico de auditoria, sequences, leases e fencing tokens.

## Atores e confiança

| Ator | Autoridade |
|---|---|
| Cluster Operator | Controla instalação, configuração do Agent e políticas do cluster; fica fora da autorização cotidiana do produto. |
| Installation Administrator | Ator humano que governa a instalação e cria Workspaces no alpha. |
| Workspace Provisioner | Futuro ator não humano limitado ao caso de uso compartilhado de criação. |
| Workspace Owner/Member/Viewer | Atores limitados por ancestry e permissões atômicas. |
| Control plane | Fonte do produto, ponto de autorização, coordenador e custodiante de secrets no modo alpha. |
| Cluster Agent | Executor e observador autenticado de um cluster; não é autoridade de política. |
| Reconciler do limite | Autoridade cluster-scoped de bootstrap, sem responsabilidade por secrets ou runtime. |
| Platform Operator | Reconcilia CRDs fechados em namespaces prontos. |
| Aplicação | Código não confiável que acessa apenas valores entregues intencionalmente. |
| Provider externo | Sistema independente que satisfaz um contrato tipado. |

## Limites e fluxo

```text
[Browser ou automação]
          |
          | HTTPS + autenticação do ator
          v
[Control plane] ---- [PostgreSQL: metadados e auditoria]
      |   |
      |   +---- TLS/workload identity ---- [SecretValueStore externo]
      |
      +---- mTLS iniciado outbound ---- [Cluster Agent]
                                            |
                                            v
                                     [API Kubernetes]
                                       |          |
                          [WorkspacePlacement]   [Secret versionado]
                                       |          |
                            [Reconciler limite]   v
                                  Namespace/RBAC [Platform Operator]
                                                      |
                                                      v
                                               [Pod da aplicação]
```

O control plane resolve o alvo antes do dispatch. O Agent valida identidade do
cluster, ownership de Workspace/namespace/runtime, sessão, sequence, deadline e
limites do payload. O Operator aceita schema fechado e não recebe valores ou
credenciais de providers.

## Registro de ameaças

| ID | STRIDE / OWASP | Cenário | Controles obrigatórios | Verificação |
|---|---|---|---|---|
| TM-01 | Spoofing / A07 | Sessão humana roubada atua em outro tenant. | Cookie seguro, CSRF, invalidação, ancestry completa e auditoria. | Integração HTTP negativa entre dois Workspaces. |
| TM-02 | Elevation / A01 | Disponibilidade do Agent é tratada como permissão. | Capability, consentimento, autorização e admission separados. | Matriz pura e negação direta pela API. |
| TM-03 | Elevation / A01/A02 | Token do Agent/Operator alcança todos namespaces. | RoleBindings por Workspace; cluster-wide apenas para recursos inerentemente globais. | SelfSubjectAccessReview positivo dentro e negativo fora. |
| TM-04 | Elevation / A01 | Input remoto aponta recurso Kubernetes arbitrário. | Comandos tipados, sem YAML/GVR/selectors, placement pronto e namespaces reservados bloqueados. | Unit/envtest com alvo estrangeiro. |
| TM-05 | Tampering / A08 | Replay ou worker antigo aplica versão regressiva. | mTLS, session, sequence, deadline, idempotência, desired version, lease e fencing. | Testes TLS de replay, takeover e reconnect. |
| TM-06 | Disclosure / A04 | Secret aparece no PostgreSQL, API ou logs. | Store externo, handle opaco, API write-only, redaction e erros sanitizados. | Testes de parâmetros e sentinelas de logs/snapshots. |
| TM-07 | Disclosure / A01/A02 | Agent lista Secrets ou lê credenciais de sistema. | Sem list/watch; get/create/delete namespaced; namespaces de sistema separados. | Contrato RBAC e negação real. |
| TM-08 | Disclosure / A04 | etcd ou node expõe Secret materializado. | Encryption at rest, etcd protegido, hardening, rotação e retenção limitada. | Warning no `doctor` e evidência do operador, sem alegar enforcement. |
| TM-09 | Disclosure | Aplicação exfiltra seu próprio secret. | Secret por AppEnvironment, mínimo valor, rotação, rede quando disponível e sem token Kubernetes. | Render/smoke; risco residual aceito. |
| TM-10 | Tampering / A06 | AppDeployment evolui para escape por PodSpec. | CRD fechado, ServiceAccount fixa, security context restrito e sem volumes/host settings arbitrários. | Testes de schema e rendering. |
| TM-11 | DoS / A10 | Automação cria namespaces/operações sem limite. | Idempotência, rate, clusters/classes permitidos, quotas e concorrência. | Testes de evento duplicado e quota antes do ator existir. |
| TM-12 | Repudiation / A09 | Operação privilegiada não é atribuível. | Audit de ator, permissão, ancestry, cluster, request/idempotency, resultado e reason. | Integração de audit para sucesso e negação. |
| TM-13 | Supply chain / A03/A08 | Imagem/chart comprometido obtém credencial privilegiada. | Digests, provenance/assinatura, SBOM, scanning e ServiceAccounts separados. | Gates de release e conformance. |
| TM-14 | SSRF / A01 | Endpoint de provider fornecido por Workspace alcança rede interna. | Endpoints só por configuração do operador, schemes/hosts tipados e sem URL arbitrária em comandos. | Validação do adaptador e bloqueio de metadata/private targets. |
| TM-15 | Exceptional / A10 | Falha parcial deixa namespace privilegiado ou utilizável cedo. | State machine por conditions, retry seguro, ownership, política explícita de deleção e sucesso somente completo. | Matriz envtest de interrupção/recuperação. |
| TM-16 | Tampering / A01/A08 | Discovery ou evidência comprometida do Agent ativa binding malicioso ou não desejado de storage, publicação ou telemetria. | Binding tipado no Control Plane por fluxo autenticado do Cluster Operator; evidência do Agent é read-only e não ativa candidatos. | Testes de autorização/admission da API e teste negativo de observação para mutação de binding. |

## Evidências atuais e gaps

Já existem sessão/CSRF, autorização de instalação e Workspace, idempotência,
identidade mTLS, session/sequence, fencing de versão desejada, configuração
imutável, ownership, rendering fechado e automount de ServiceAccount desabilitado
para aplicações.

A decisão namespaced de provisionamento, o boundary controller de
`WorkspacePlacement`, a separação das credenciais de runtime/discovery, a entrega
imutável just-in-time de secrets e os limites sanitizados de transporte e
persistência estão implementados e validados no K3s alpha. O Workspace legado de
teste ainda usa namespace compartilhado; sua transição aprovada é teardown e
recriação explícitos, não migração.

Backend externo de secrets e evidência de encryption at rest sob responsabilidade
do operador não estão configurados no K3s atual. Permanecem capabilities opcionais
e não tornam o application loop core indisponível.

## Invariantes

- Feature Availability não contém autorização de ator.
- Consentimento do Cluster Operator não é modificável por ator comum do control plane.
- Criação humana e automatizada compartilham o mesmo caso de uso.
- Agent/Operator não aceitam PodSpec, RoleBinding, ServiceAccount, selector ou API path do usuário.
- Workspace só fica pronto após ownership e ambos bindings namespaced.
- Secrets não aparecem em APIs de leitura ou storage durável do control plane.
- Operator permanece secret-blind; Agent não possui list/watch de Secrets.
- Falha opcional de provider não muda desired state nem derruba o core.
- Observações do Agent e candidatos descobertos não criam nem ativam bindings no
  Control Plane.
- Administrador Kubernetes sempre pode sobrepor controles e fica fora do isolamento tenant.

## Riscos aceitos e não objetivos

- Alphas podem exigir reinstalação limpa.
- Namespace não é hard multi-tenancy contra workloads hostis, escape de container,
  kernel comprometido ou administrador malicioso.
- O control plane vê plaintext no modo alpha.
- Aplicações podem divulgar os secrets que recebem.
- Molejo não configura automaticamente etcd, KMS, nodes, CNI, IAM cloud ou rede do cluster.

## Revisão

Cada cut deve associar mudanças de confiança a IDs deste documento e adicionar o
teste de menor custo que detecta regressão. A revisão é obrigatória ao adicionar
ator, permissão, credencial, callback, command kind, campo de CRD, privilégio de
ServiceAccount, backend ou modo de entrega.

## Referências

- [ADR 0019](../adr/0019-provisionamento-de-workspace-e-limite-de-namespace.md)
- [ADR 0020](../adr/0020-custodia-de-secrets-e-entrega-ao-runtime.md)
- [ADR 0021](../adr/0021-bindings-explicitos-gerenciados-pelo-operador.md)
- [ADR 0022](../adr/0022-metricas-neutras-com-consulta-prometheus.md)
- [OWASP Threat Modeling](https://cheatsheetseries.owasp.org/cheatsheets/Threat_Modeling_Cheat_Sheet.html)
- [OWASP Top 10: 2025](https://owasp.org/Top10/2025/0x00_2025-Introduction/)
- [Boas práticas RBAC Kubernetes](https://kubernetes.io/docs/concepts/security/rbac-good-practices/)
- [Boas práticas de Secrets Kubernetes](https://kubernetes.io/docs/concepts/security/secrets-good-practices/)
