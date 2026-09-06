# Acesso ao registry de aplicações

`RegistrySetup` é um runbook local e versionado do `molejoctl`. Ele prepara um
namespace existente para baixar imagens de aplicações de um registry privado.
Não é uma CRD do Kubernetes, não é armazenado pelo control plane e não configura
nodes, IAM, container runtimes, DNS ou rotas de rede.

Gere e inspecione o arquivo sem credenciais:

```sh
molejoctl capability registry init \
  --host registry.example.com \
  --namespace meu-namespace \
  --secret-name application-registry \
  --probe-image registry.example.com/apps/probe@sha256:<digest> \
  --output registry-setup.yaml

molejoctl capability registry plan \
  --kube-context meu-cluster \
  --file registry-setup.yaml \
  --from-docker-config ~/.docker/config.json
```

Depois de revisar o plano, aplique e verifique o resultado:

```sh
molejoctl capability registry apply \
  --kube-context meu-cluster \
  --file registry-setup.yaml \
  --from-docker-config ~/.docker/config.json \
  --yes

molejoctl capability registry verify --kube-context meu-cluster --file registry-setup.yaml
molejoctl capability registry smoke --kube-context meu-cluster --file registry-setup.yaml
```

O `apply` filtra o Docker config para o registry selecionado, cria um Secret
`kubernetes.io/dockerconfigjson` gerenciado e acrescenta sua referência à
ServiceAccount existente. Ele não cria o namespace ou a ServiceAccount e não
sobrescreve um Secret que pertença a outra ferramenta. O `smoke` usa
`imagePullPolicy: Always` e sempre remove seu Pod efêmero.

As credenciais podem ser recebidas pelo stdin com `--from-docker-config -`; elas
nunca devem entrar no `RegistrySetup`, em argumentos ou no Git. Use credenciais
pull-only e faça a rotação executando novamente `plan`, `apply` e `smoke`.

Este runbook trata somente o pull das imagens das aplicações. Credenciais OCI do
Helm e as imagens dos próprios componentes Molejo são fluxos separados. Um
namespace criado posteriormente exige uma nova execução explícita ou um
mecanismo contínuo escolhido pelo operador do cluster.
