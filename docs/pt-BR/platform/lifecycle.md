# Ciclo de vida da plataforma

O platform lifecycle cuida apenas dos componentes e contratos da Molejo:

```sh
molejoctl platform runtime install --kube-context <contexto> --version <versão>
molejoctl platform control-plane install --kube-context <contexto> --version <versão>
molejoctl platform doctor --kube-context <contexto>
molejoctl platform status --kube-context <contexto>
```

O runtime é composto pelo Platform Operator e pelo Cluster Agent outbound. O
control plane contém API, Console, configuração do PostgreSQL e material de pairing
instalados pelo chart alpha atual. `doctor` e `status` validam o contrato do runtime.

Releases alpha não prometem upgrades in-place. Se a release instalada ou sua
configuração pública imutável for diferente, faça backup do que precisa ser
preservado, siga o teardown experimental e reinstale o alpha solicitado.
