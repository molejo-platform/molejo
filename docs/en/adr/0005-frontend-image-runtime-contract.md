# ADR-0005: Frontend Image Runtime Contract

## Status

Draft

## Context

The platform must prove that static sites and browser SPAs can use the same
`AppDeployment` lifecycle already used by HTTP backends. Encoding a frontend
framework or build tool in the Kubernetes API would couple runtime reconciliation
to source-build choices and unnecessarily expand the public contract.

Frontend images must also work with the restricted workload runtime: non-root,
read-only root filesystem, no capabilities, immutable OCI references, and fixed
HTTP probes.

## Decision

`static-html` and `vite-react-spa` are platform-maintained reference image
contracts, not values in `AppDeploymentSpec`. The operator accepts any immutable
HTTP image that satisfies the declared port, probes, resources, and restricted
container runtime; it does not inspect the frontend framework or HTTP server
inside that image. Both reference profiles expose HTTP on port `8080`, provide
`/healthz` and `/readyz`, and run repository-controlled NGINX as UID/GID
`65532:65532`. Runtime temporary files remain under `/dev/shm`.

The static profile serves independent HTML documents and returns `404` for
unknown paths. The SPA profile serves Vite build artifacts and returns
`index.html` with HTTP `200` for browser routes unknown to NGINX. The client-side
router owns the decision to render a route or a Not Found page. Missing assets
return `404` and never receive the SPA shell. HTML uses `Cache-Control: no-cache`
and is revalidated; fingerprinted assets use a one-year immutable cache policy.

The repository contains minimal fixtures for both contracts. The Vite fixture is
a pnpm workspace consumer used to prove build and runtime behavior; it is not the
Fruto product web application. It proves the server fallback but does not provide
a router-specific Not Found page. Images are deployed only by immutable digest.

## Consequences

Static sites and SPAs reuse Deployment, Service, HTTPRoute, status, rollout,
drift correction, and garbage collection without any CRD or controller change.
NGINX policy remains reviewable and testable by the platform.

Applications using these reference profiles cannot inject arbitrary NGINX
directives through `AppDeployment`. Developers remain free to publish a custom
HTTP image with another server or routing policy. SSR, source builds from Git,
CDN integration, framework detection, and user-selected runtime profiles in the
Kubernetes API remain outside this decision.

## Alternatives Considered

Add `spec.profile` to `AppDeployment`. Rejected because the operator needs only a
valid HTTP image contract and should not understand how frontend assets were
built.

Allow each application to inject NGINX configuration into the maintained
reference profiles through `AppDeployment`. Rejected because it would enlarge
the public API, security, and compatibility surface before a real consumer
requires it. A fully custom image remains supported.

Use one fallback policy for every file. Rejected because returning `index.html`
for missing JavaScript or CSS hides deployment errors and breaks cache semantics.

## References

- [ADR-0003: Private Stateless Backend Runtime Contract](0003-private-stateless-backend-runtime-contract.md)
- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
- [Vite static deployment guide](https://vite.dev/guide/static-deploy.html)
- [NGINX headers module](https://nginx.org/en/docs/http/ngx_http_headers_module.html)
