# TLS do cluster

`molejoctl cluster tls configure` valida ou provisiona o material de certificado e grava um `ClusterTLSBinding` reutilizável. Ele não instala um Gateway, publica aplicações nem gerencia os registros DNS das aplicações.

Use o exemplo de Secret externo quando o ciclo de vida do certificado for gerenciado fora do Molejo:

```bash
molejoctl cluster tls configure \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-existing-secret.yaml
```

A primeira execução mostra o plano. Aplique-o explicitamente com `--yes`. Uma nova execução deve informar que o profile foi verificado sem exigir aprovação.

O driver `existing-secret` exige um Secret `kubernetes.io/tls` cuja chave corresponda ao certificado, cuja validade seja superior a 24 horas e cujos SANs cubram todos os domínios configurados.

Nesta primeira versão do contrato, o driver `cert-manager` suporta ACME DNS-01 com Cloudflare. O Secret referenciado deve existir previamente no namespace `cert-manager`, com o token de API na chave `api-token`. O Molejo nunca armazena o token no profile nem o imprime. Comece pelo exemplo de staging em `deploy/examples/tls-cert-manager-cloudflare.yaml` antes de alterar o ambiente do issuer para `production`.

O binding resultante é armazenado como `molejo-system/molejo-tls-<profile>`. `molejoctl cluster doctor` valida todos os bindings armazenados e seus Secrets TLS atuais.
