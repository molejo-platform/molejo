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

Query keys and `queryOptions` belong to the owning feature. Every variable that
changes a response must be present in its key, GET requests must propagate the
query cancellation signal, and cache freshness must describe the resource
lifecycle instead of inheriting one application-wide interval.

Route parents mirror the product hierarchy and keep long-lived layouts mounted
while sibling pages change. Realtime transport is isolated from page rendering:
metrics are shared for the active runtime, while live logs remain an explicit,
page-scoped action and resume from a short-lived session cursor after reload.

## Molejo boundaries

The Console operates the application loop: Workspaces, Projects, Applications,
Environments, App Environments, releases, deployments, runtime configuration, and
observability. Provider-specific systems such as GitHub are integrations. The
Console does not administer Kubernetes nodes, cluster networking, or capability
installation runbooks.

## File policy

- Production-authored files must remain at or below 400 lines.
- A range of 150-300 lines is preferred, but splitting by line count alone is not.
- Tests and generated OpenAPI files are exempt from the hard limit, but still split by behavior when navigation becomes
  difficult.

## Visual architecture

The Console uses native CSS, cascade layers, semantic custom properties, and CSS
Modules for private component or feature styles. It does not maintain a parallel
utility framework, preprocessor, or runtime theme provider.

- `shared/design/theme-molejo.css` owns shared visual values and semantic roles.
- `shared/design/base.css` owns document defaults and native element behavior.
- `shared/ui` owns reusable component contracts and their styles.
- `app` owns the stylesheet composition root, the application shell, and
  viewport-level responsive changes.
- A feature owns layouts that express its product vocabulary and imports its own
  stylesheet in the `features` cascade layer.
- A visual component exported to another feature imports its styles directly;
  route-level stylesheets cannot provide the visual contract of public components.
- Interactive behavior that is difficult to implement accessibly may use Base UI,
  but only behind a contract in `shared/ui`. Features must not import Base UI.
- Icons use the local `Icon` contract. Lucide is an implementation detail and must
  not be imported outside `shared/ui`.

Use grid for aligned, two-dimensional relationships such as field groups,
cards, and filters. Use flexbox for one-dimensional relationships such as
navigation and action groups. Viewport media queries are reserved for shell
changes; reusable components respond to their containing block with container
queries.

Create a React component when semantics, behavior, or supported variants repeat.
Use a named CSS layout contract when only geometry repeats. A local dimension
becomes a token only after it represents shared product knowledge.

Native controls remain the default. React Hook Form and Zod are reserved for
multi-step or structurally complex workflows; simple forms keep native HTML and
local React state. The API remains the authority for business validation.

## Browser support

The authored frontend targets current evergreen browsers with the following
minimums: Chromium 111, Firefox 128, and Safari 16.4. Critical journeys must be
usable with keyboard navigation, reduced motion, and mobile viewports. Automated
accessibility checks are a regression gate, not a substitute for manual assistive
technology review.

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
