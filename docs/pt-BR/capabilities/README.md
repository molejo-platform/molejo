# Capacidades do cluster

Capacidades conectam infraestrutura escolhida pelo operador do cluster à Molejo.
Cada capacidade possui ownership explícito:

- `external`: a Molejo apenas consome o resultado;
- `runbook-managed`: o `molejoctl` aplica uma receita local revisada;
- `molejo-managed`: o recurso faz parte da release da Molejo;
- `provider-managed`: o provedor Kubernetes ou cloud controla seu ciclo de vida.

Os runbooks atuais são [Gateway com Traefik](gateway-traefik.md),
[TLS com cert-manager](tls-cert-manager.md), [acesso ao registry](registry.md) e
[verificação de armazenamento Kubernetes](storage.md).
Eles usam `init`, `plan`, `apply`, `verify` e, quando fizer sentido, `smoke`. Um
runbook não é uma API de plugins, não se torna recurso do control plane e nunca
transfere credenciais do provedor ao Platform Operator ou Cluster Agent.
