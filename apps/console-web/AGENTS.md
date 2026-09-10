# Console development

The Console owns the browser experience for Molejo product APIs. It does not
talk directly to Kubernetes or provider APIs.

## Explore

- Read [`ARCHITECTURE.md`](ARCHITECTURE.md) for dependency and source ownership rules.
- Start application composition in `src/app/`, vertical product slices in
  `src/features/`, and domain-neutral code in `src/shared/`.
- Treat `src/shared/api/generated/control-plane.ts` as generated from
  `../../contracts/openapi/control-plane-v1.yaml`.

## Boundaries

- Keep product features vertical and export cross-feature contracts through
  `public.ts`.
- Keep transport, query cache, session, and stream lifecycles outside visual
  components.
- Change the OpenAPI source before adapting the generated client or API-facing UI.
- Follow [`../../docs/en/CONTRIBUTING.md`](../../docs/en/CONTRIBUTING.md) for commands
  and testing strategy.
