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

## Administração da publicação HTTP

A instalação do Control Plane imprime o Cluster ID não secreto e o UID atual do
Kubernetes. Salve o Cluster ID em um setup versionado e mantenha o contexto
Kubernetes explícito para que o molejoctl bloqueie um cluster recriado ou incorreto
antes de alterar o produto.

```yaml
apiVersion: config.molejo.dev/v1alpha1
kind: HTTPPublicationSetup
metadata:
  name: molejo-dev
spec:
  clusterId: cls-abcdefghijklmnopqrst
  binding:
    schemaVersion: kubernetes-http.v1alpha1
    gatewayNamespace: molejo-system
    gatewayName: molejo
    listeners:
      - name: https-apex
        hostname: molejo.dev
      - name: https-apps
        hostname: "*.molejo.dev"
  domains:
    - id: home
      kind: Exact
      name: molejo.dev
      reservedNames: []
      workspaceIds:
        - ws-abcdefghijklmnopqrst
    - id: apps
      kind: SubdomainPool
      name: molejo.dev
      reservedNames:
        - admin.molejo.dev
      workspaceIds:
        - ws-abcdefghijklmnopqrst
```

Execute o plan somente leitura, revise as operações e então aplique e verifique:

```sh
molejoctl capability publication plan --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml
molejoctl capability publication apply --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml --yes
molejoctl capability publication verify --control-plane https://cloud.molejo.dev --username owner --kube-context molejo-k3s --file publication.yaml
```

Use `--ca-file` com uma CA privada do Control Plane. As credenciais são solicitadas
somente em um terminal interativo. `status` e `dependents` leem o mesmo estado da
API; listas retornam uma página limitada e um cursor explícito. A remoção nunca é
inferida do arquivo. Use `grant revoke`, `domain delete` e `binding disconnect`
após remover referências Desired, Applied e Executable. Não existe opção force.

O Binding declara listeners existentes do Gateway. Ele não instala um Gateway,
emite ou renova certificados, edita DNS nem comprova alcance público. Essas
responsabilidades continuam com os owners de infraestrutura configurados; nomes
de DNS privado são válidos quando roteamento e grants declarados forem válidos.
