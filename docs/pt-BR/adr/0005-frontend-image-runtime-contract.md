# ADR-0005: Frontend Image Runtime Contract

## Status

Draft

## Context

A plataforma precisa comprovar que sites estáticos e SPAs executadas no navegador
podem usar o mesmo ciclo de vida de `AppDeployment` já usado por backends HTTP.
Codificar um framework de frontend ou ferramenta de build na API Kubernetes
acoplaria a reconciliação de runtime a escolhas de build e ampliaria o contrato
público sem necessidade.

As imagens também precisam funcionar com o runtime restrito: non-root, root
filesystem somente leitura, sem capabilities, referências OCI imutáveis e probes
HTTP fixas.

## Decision

`static-html` e `vite-react-spa` são contratos de imagem de referência mantidos
pela plataforma, não valores de `AppDeploymentSpec`. O operator aceita qualquer
imagem HTTP imutável que satisfaça porta, probes, resources e runtime restrito
declarados; ele não inspeciona o framework de frontend nem o servidor HTTP dentro
da imagem. Os dois perfis de referência expõem HTTP na porta `8080`, oferecem
`/healthz` e `/readyz` e executam o NGINX controlado pelo repositório como UID/GID
`65532:65532`. Arquivos temporários permanecem em `/dev/shm`.

O perfil estático serve documentos HTML independentes e responde `404` para paths
desconhecidos. O perfil SPA serve artefatos do build Vite e retorna `index.html`
com HTTP `200` para rotas do navegador desconhecidas pelo NGINX. O roteador no
cliente decide se renderiza uma rota ou uma página Not Found. Assets ausentes
retornam `404` e nunca recebem o shell da SPA. HTML usa
`Cache-Control: no-cache` e é revalidado; assets com fingerprint usam cache
imutável por um ano.

O repositório contém fixtures mínimas para os dois contratos. A fixture Vite é um
consumer do workspace pnpm usado para comprovar build e runtime; ela não é a
aplicação web do produto Molejo. Ela comprova o fallback do servidor, mas não
fornece uma página Not Found específica de um roteador. As imagens são implantadas
somente por digest.

## Consequences

Sites estáticos e SPAs reutilizam Deployment, Service, HTTPRoute, status, rollout,
correção de drift e garbage collection sem alterar CRD ou controller. A política
NGINX permanece revisável e testável pela plataforma.

Aplicações que usam esses perfis de referência não podem injetar diretivas NGINX
arbitrárias pelo `AppDeployment`. Desenvolvedores continuam livres para publicar
uma imagem HTTP própria com outro servidor ou política de rotas. SSR, build a
partir do Git, CDN, detecção de framework e seleção de perfil pelo usuário na API
Kubernetes ficam fora desta decisão.

## Alternatives Considered

Adicionar `spec.profile` ao `AppDeployment`. Rejeitado porque o operator precisa
apenas de um contrato de imagem HTTP válido e não deve conhecer como os assets
foram construídos.

Permitir que cada aplicação injete configuração NGINX nos perfis de referência
mantidos por meio do `AppDeployment`. Rejeitado porque ampliaria a API pública e a
superfície de segurança e compatibilidade antes de existir um consumidor real.
Uma imagem totalmente própria continua suportada.

Usar uma política de fallback para todos os arquivos. Rejeitado porque retornar
`index.html` para JavaScript ou CSS ausente oculta falhas e quebra o cache.

## References

- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
- [Guia de deploy estático do Vite](https://vite.dev/guide/static-deploy.html)
- [Módulo de headers do NGINX](https://nginx.org/en/docs/http/ngx_http_headers_module.html)
