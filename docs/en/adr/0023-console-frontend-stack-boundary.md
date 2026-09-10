# ADR 0023: Console frontend stack boundary

## Status

Accepted for the alpha architecture.

## Context

The Console already has semantic Molejo tokens, native CSS cascade layers,
feature-owned styles, and React component contracts. Its maintenance pressure is
concentrated in complex interactive behavior, forms, and large route files rather
than missing utility classes. Replacing the visual system would create migration
churn without solving focus management, accessibility, or domain ownership.

## Decision

The Console keeps React, TypeScript, Vite, TanStack Router, TanStack Query,
TanStack Virtual, native CSS, semantic custom properties, and cascade layers.
CSS Modules scope private component and feature styles; shared visual contracts
remain global.

Base UI may provide complex accessible behavior only through adapters in
`shared/ui`. Lucide icons are also exposed only through the local `Icon` contract.
Native HTML remains the default. React Hook Form and Zod are limited to
multi-step or structurally complex workflows and do not replace API business
validation.

Tailwind CSS, shadcn/ui, CSS preprocessors, CSS-in-JS, runtime theme providers,
and global client-state libraries are not part of the current stack. Table,
chart, and component-workbench libraries enter only with a proven use case.

## Consequences

- Molejo keeps one semantic token source and its own visual identity.
- External behavior libraries remain replaceable behind local contracts.
- Features cannot import Base UI or Lucide directly.
- Production-authored files stay at or below 400 lines.
- New dependencies require a concrete use case, bundle measurement,
  accessibility coverage, and an exit path.
- Tailwind or shadcn may be reconsidered only through a migration that removes,
  rather than permanently duplicates, the current styling contract.

## Alternatives considered

Tailwind plus shadcn was rejected for now because it would move existing styling
into another syntax and make generated component code a local maintenance
responsibility. Sass and other preprocessors were rejected because current CSS
already supplies the required capabilities. A hand-written primitive layer was
rejected for modal interactions because focus, keyboard, inert background, and
scroll locking are specialized behavior.

## References

- [`apps/console-web/ARCHITECTURE.md`](../../../apps/console-web/ARCHITECTURE.md)
- [Base UI](https://base-ui.com/react/overview/about)
- [Vite CSS features](https://vite.dev/guide/features)
- [shadcn/ui](https://ui.shadcn.com/docs)
- [Tailwind CSS v4](https://tailwindcss.com/blog/tailwindcss-v4)
