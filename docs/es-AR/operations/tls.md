# TLS del clúster

`molejoctl cluster tls configure` valida o provisiona el material del certificado y escribe un `ClusterTLSBinding` reutilizable. No instala un Gateway, no publica aplicaciones y no administra los registros DNS de las aplicaciones.

Usá el ejemplo de Secret externo cuando el ciclo de vida del certificado se administra fuera de Molejo:

```bash
molejoctl cluster tls configure \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-existing-secret.yaml
```

La primera ejecución muestra el plan. Aplicalo explícitamente con `--yes`. Una nueva ejecución debe indicar que el profile fue verificado sin requerir aprobación.

El driver `existing-secret` requiere un Secret `kubernetes.io/tls` cuya clave coincida con el certificado, cuya validez supere 24 horas y cuyos SANs cubran todos los dominios configurados.

En esta primera versión del contrato, el driver `cert-manager` soporta ACME DNS-01 con Cloudflare. El Secret referenciado debe existir previamente en el namespace `cert-manager`, con el token de API en la clave `api-token`. Molejo nunca almacena el token en el profile ni lo imprime. Empezá con el ejemplo de staging en `deploy/examples/tls-cert-manager-cloudflare.yaml` antes de cambiar el ambiente del issuer a `production`.

El binding resultante se almacena como `molejo-system/molejo-tls-<profile>`. `molejoctl cluster doctor` valida todos los bindings almacenados y sus Secrets TLS actuales.
