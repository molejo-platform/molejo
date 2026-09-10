# TLS del clúster

Molejo es responsable de la política de dominios públicos y consume Secrets TLS
estándar de Kubernetes. La emisión y renovación de certificados siguen siendo
responsabilidad del operador del clúster y del controlador de certificados
elegido. El Control Plane, Platform Operator y Cluster Agent nunca reciben
credenciales del Provider DNS.

`molejoctl capability tls verify` es read-only y valida que el Secret `kubernetes.io/tls` tenga una clave compatible, una validez mínima de 24 horas y cobertura para todos los nombres DNS de un documento local `TLSSetup`:

```bash
molejoctl capability tls verify \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-existing-secret.yaml
```

`molejoctl capability tls prepare` es una receta opcional para el day zero. La primera receta instala cert-manager y solicita un certificado Let's Encrypt mediante Cloudflare DNS-01. cert-manager queda a cargo de la renovación cuando el comando termina.

Ejecutá staging antes de production:

```bash
molejoctl capability tls prepare \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-molejo-dev-staging.yaml \
  --credential-env CLOUDFLARE_API_TOKEN \
  --yes

molejoctl capability tls prepare \
  --kube-context molejo-k3s \
  --file deploy/examples/tls-molejo-dev-production.yaml \
  --credential-env CLOUDFLARE_API_TOKEN \
  --yes
```

El token se almacena en `cert-manager/cloudflare-dns-token`, en la clave `api-token`, y nunca se escribe en el setup ni en la salida del comando. Necesita `Zone - DNS - Edit` y `Zone - Zone - Read` para `molejo.dev`. El account ID de Cloudflare no es utilizado por el solver de cert-manager basado en API token.

Staging y production usan recursos Certificate y Secrets separados. El resultado production es `molejo-system/molejo-dev-tls`, con cobertura para `molejo.dev`, `*.molejo.dev` y `*.stateful.molejo.dev`.

El archivo `TLSSetup` es una receta local de `molejoctl`, no una API del Control
Plane o de los workloads. No instala un Gateway, no conecta el Secret a un
listener, no crea registros DNS permanentes para aplicaciones ni persiste un
Binding TLS específico de Molejo. El futuro contrato de consumo será
`certificateRefs` de Gateway API.
