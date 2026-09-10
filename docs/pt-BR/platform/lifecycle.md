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

## Charts locais para desenvolvimento

Os dois comandos `install` aceitam `--chart-path` para executar o mesmo fluxo com
um diretório ou arquivo `.tgz` local já preparado. O chart deve usar a API `v2`,
ter o nome esperado (`molejo-cluster` ou `molejo-control-plane`) e possuir a mesma
versão informada em `--version`.

Os diretórios em `deploy/charts` são insumos da ferramenta de release e não são
diretamente instaláveis: o artefato preparado também contém os manifests, CRDs e
digests de imagens gerados durante o empacotamento. Charts locais são tratados
como imutáveis; para testar outro conteúdo sob a mesma versão alpha, remova a
instalação experimental e reinstale-a.
