# Capacidade de Gateway para K3s

`GatewaySetup` é uma receita local e versionada do `molejoctl`. Ela não é uma CRD
do Kubernetes e não é armazenada pelo control plane. O perfil inicial `k3s`
gerencia uma release fixada do Traefik, uma `GatewayClass` e o `Gateway` HTTPS
compartilhado pelas rotas da Molejo. DNS, firewall, proxy reverso e load balancer
permanecem fora desse contrato.

Os pré-requisitos são um contexto K3s acessível, os componentes de cluster da
Molejo com as CRDs necessárias do Gateway API e um Secret TLS existente no
namespace do Gateway. Gere, inspecione, aplique e verifique o setup com:

```sh
molejoctl capability gateway init \
  --profile k3s \
  --domain molejo.dev \
  --certificate-secret molejo-system/molejo-dev-tls \
  --output gateway-setup.yaml

molejoctl capability gateway plan --kube-context molejo-k3s --file gateway-setup.yaml
molejoctl capability gateway apply --kube-context molejo-k3s --file gateway-setup.yaml --yes
molejoctl capability gateway verify --kube-context molejo-k3s --file gateway-setup.yaml
```

O arquivo gerado pode ser versionado porque não contém credenciais. `plan` e
`verify` são somente leitura. `apply` gerencia apenas a release Traefik gerada e
o Gateway compartilhado identificado por label; o comando recusa adotar Gateway,
GatewayClass ou NodePort que pertençam a outro componente.

Em uma instalação nova do control plane, conecte o console ao Gateway com:

```sh
molejoctl platform control-plane install \
  --kube-context molejo-k3s \
  --public-host cloud.molejo.dev \
  --gateway molejo-system/molejo \
  --gateway-section https-molejo
```

O `HTTPRoute` resultante expõe apenas o `console-web`. O console encaminha
`/api/*` para a API interna do control plane.

A receita `config.molejo.dev/v1alpha2` exige `instance.listeners` com 1–10
entradas. Cada uma declara `name`, `hostname` exato ou wildcard e
`certificateSecret`. `init` produz apex (`https-apex`) e wildcard
(`https-molejo`). Receitas antigas são recusadas; regenere na instalação alpha
limpa. O Secret deve cobrir todos os nomes. Os listeners gerenciados permitem
HTTPRoutes apenas de namespaces com
`platform.molejo.dev/http-publication=enabled`, atribuída pelo provisionamento
de Workspace e pela instalação do Control Plane.

`verify` verifica conformidade com a receita gerenciada. A biblioteca separada
`InspectConsumption` avalia listener, attachment, suporte HTTPRoute e condições
atuais de Gateway externo usando leitura, sem Helm ou Secret. O comando integrado
com APIs de bindings pertence à Fase 3. Gateway externo conectado não recebe patch;
o Operator continua gerenciando suas HTTPRoutes.

Veja [fundação de publicação HTTP](../../en/architecture/http-publication.md) para
limites, versões, evidências de estado e prova local TLS.
