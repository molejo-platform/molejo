# Capacidades del clúster

Las capacidades conectan infraestructura elegida por el operador del clúster con
Molejo. Cada capacidad tiene ownership explícito:

- `external`: Molejo solamente consume el resultado;
- `runbook-managed`: `molejoctl` aplica una receta local revisada;
- `molejo-managed`: el recurso forma parte de la release de Molejo;
- `provider-managed`: el proveedor Kubernetes o cloud administra su ciclo de vida.

Los runbooks actuales son [Gateway con Traefik](gateway-traefik.md),
[TLS con cert-manager](tls-cert-manager.md) y [acceso al registry](registry.md).
Usan `init`, `plan`, `apply`, `verify` y, cuando corresponde, `smoke`. Un runbook no
es una API de plugins, no se convierte en recurso del control plane y nunca entrega
credenciales del proveedor al Platform Operator o Cluster Agent.
