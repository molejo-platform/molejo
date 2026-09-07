# Console architecture

The Console is the Molejo Control Plane interface. Its source tree must expose the
product capabilities an operator and developer use, rather than framework layers
or navigation positions.

## Source boundaries

```text
src/
├── app/       composition, providers, routing, navigation, and application shell
├── features/  vertical Molejo capabilities and their owned UI, API, and state
├── shared/    domain-neutral API infrastructure, design, UI, and utilities
└── test/      shared test harnesses and factories
```

Dependencies point inward as `app -> features -> shared`:

- `shared` cannot import `features` or `app`.
- `app` composes the application and imports feature public contracts.
- A feature can import another feature only through that feature's `public.ts`.
- Cross-feature deep imports and static import cycles are prohibited.
- Generated OpenAPI operations and schemas are exposed through shared HTTP types and must not be imported by pages.

## Feature ownership

A feature owns the product vocabulary it names. It starts with a flat structure
and adds files only as responsibilities emerge:

```text
features/delivery/
├── api.ts       transport operations
├── queries.ts   query keys and, when cohesive, options, invalidation, and mutation workflows
├── model.ts     pure validation, transitions, and payload builders
├── ...Page.tsx  route-level coordinators and views
└── public.ts    the minimal contract available to other features and app
```

Do not introduce repository classes, use-case classes, dependency-injection
containers, global stores, or a generic CRUD/form engine. TanStack Query owns
server state, the router owns navigable state, and components own transient UI
state.

## Molejo boundaries

The Console operates the application loop: Workspaces, Projects, Applications,
Environments, App Environments, releases, deployments, runtime configuration, and
observability. Provider-specific systems such as GitHub are integrations. The
Console does not administer Kubernetes nodes, cluster networking, or capability
installation runbooks.

## File policy

- Authored files must remain below 1,000 lines.
- Files over 400 lines require an explicit cohesion review.
- A range of 150-300 lines is preferred, but splitting by line count alone is not.
- Generated OpenAPI files are exempt.

## Adding a capability

1. Choose the feature that owns the resource or workflow; create a new feature
   only when the product vocabulary has a new owner.
2. Put transport-only code in `api.ts`, cache policy in `queries.ts`, and pure
   decisions in `model.ts`.
3. Export only cross-feature and routing contracts from `public.ts`.
4. Add focused unit tests for pure behavior and page integration tests for visible
   behavior. Prefer the real router and QueryClient over mocking internal modules.
5. Run `pnpm architecture:check`, `pnpm lint`, `pnpm check`, `pnpm test`, and
   `pnpm build` before opening a review.
