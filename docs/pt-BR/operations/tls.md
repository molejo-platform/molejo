# TLS do cluster

A Molejo é responsável pela política de domínios públicos e consome Secrets TLS padrão do Kubernetes. A emissão e a renovação dos certificados continuam sob responsabilidade do operador do cluster e do controlador de certificados escolhido. O control plane, o platform operator e o cluster Agent nunca recebem credenciais do provedor DNS.

`molejoctl cluster tls verify` é read-only e valida se o Secret `kubernetes.io/tls` possui chave compatível, validade mínima de 24 horas e cobertura para todos os nomes DNS de um documento local `TLSSetup`:

```bash
molejoctl cluster tls verify \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-existing-secret.yaml
```

`molejoctl cluster tls prepare` é uma receita opcional de conveniência para o day zero. A primeira receita instala o cert-manager e solicita um certificado Let's Encrypt por Cloudflare DNS-01. O cert-manager é responsável pela renovação depois que o comando termina.

Execute staging antes de production:

```bash
molejoctl cluster tls prepare \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-molejo-dev-staging.yaml \
  --credential-env CLOUDFLARE_API_TOKEN \
  --yes

molejoctl cluster tls prepare \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-molejo-dev-production.yaml \
  --credential-env CLOUDFLARE_API_TOKEN \
  --yes
```

O token é armazenado em `cert-manager/cloudflare-dns-token`, na chave `api-token`, e nunca é gravado no setup ou na saída do comando. Ele precisa de `Zone - DNS - Edit` e `Zone - Zone - Read` para `molejo.dev`. O account ID da Cloudflare não é utilizado pelo solver baseado em API token do cert-manager.

Staging e production usam recursos Certificate e Secrets separados. O resultado production é `molejo-system/molejo-dev-tls`, cobrindo `molejo.dev`, `*.molejo.dev` e `*.stateful.molejo.dev`.

O arquivo `TLSSetup` é uma receita local do molejoctl, não uma API do control plane ou dos workloads. Ele não instala Gateway, conecta o Secret a um listener, cria registros DNS permanentes para aplicações ou persiste um binding TLS específico da Molejo. O futuro contrato de consumo será `certificateRefs` do Gateway API.
