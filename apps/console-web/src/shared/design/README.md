# Molejo Console design contract

The Console keeps its visual contract local while it is evolving independently.
The contract has four layers:

1. Molejo brand primitives in `theme-molejo.css`.
2. Semantic `--ui-*` roles in the same theme file.
3. Shared UI styles owned by the corresponding contracts in `shared/ui` or the
   cohesive design stylesheets.
4. Feature styles beside the feature that owns them.

Private selectors use CSS Modules when a component or feature owns them. Global
classes are reserved for shared semantic contracts such as buttons, fields,
panels, layout composition, and feedback.

Components and feature styles consume semantic roles. They must not depend on
`--color-molejo-*` primitives or author colors outside `theme-molejo.css`. This
keeps palette changes and a future validated brand theme from requiring component
changes.

Layout values stay local unless they are already repeated. A value does not become
a token merely because it can be named.

`app/styles.css` is the composition root. Shared styles must never import a
feature stylesheet. Features import their own CSS and place it in the `features`
cascade layer.

Use grid for aligned field, card, filter, and data relationships. Use flexbox
for navigation and action groups. Media queries control viewport-level shell
changes; components use container queries when their behavior depends on the
space provided by a parent.

The Console intentionally has no Tailwind, CSS preprocessor, CSS-in-JS, runtime
theme provider, or parallel design-token source. Base UI supplies behavior only,
and Lucide supplies icons only; both stay behind contracts in `shared/ui`.
Introduce another styling system only through an explicit migration decision that
removes, rather than duplicates, the current contract.

## Change checklist

1. Place color values and foreground/background pairs in `theme-molejo.css`.
2. Verify text and control contrast with `design-tokens.unit.test.ts`.
3. Preserve keyboard focus, reduced motion, and the 44 px interaction target.
4. Run the Console unit tests, integration tests, and production build.
5. Inspect the affected interface at mobile and desktop widths.

Do not add a runtime theme provider or DTCG build pipeline. Extract a shared package
only when the Console and another product require synchronized consumption rather
than merely the same brand palette.
