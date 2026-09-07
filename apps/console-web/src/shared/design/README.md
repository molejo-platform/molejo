# Molejo Console design contract

The Console keeps its visual contract local while it is evolving independently.
The contract has four layers:

1. Molejo brand primitives in `theme-molejo.css`.
2. Semantic `--ui-*` roles in the same theme file.
3. Shared component styles in `components.css`.
4. Feature styles beside the feature that owns them.

Components and feature styles consume semantic roles. They must not depend on
`--color-molejo-*` primitives or author colors outside `theme-molejo.css`. This
keeps palette changes and a future validated brand theme from requiring component
changes.

Layout values stay local unless they are already repeated. A value does not become
a token merely because it can be named.

## Change checklist

1. Place color values and foreground/background pairs in `theme-molejo.css`.
2. Verify text and control contrast with `design-tokens.unit.test.ts`.
3. Preserve keyboard focus, reduced motion, and the 44 px interaction target.
4. Run the Console unit tests, integration tests, and production build.
5. Inspect the affected interface at mobile and desktop widths.

Do not add a runtime theme provider or DTCG build pipeline. Extract a shared package
only when the Console and another product require synchronized consumption rather
than merely the same brand palette.
