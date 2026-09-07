# Console Design System Foundation

## Objective

Turn the Console's existing visual language into a small, explicit, and testable
design contract while preserving the Molejo palette and typography documented in
`internal`.

## Scope

1. Separate Molejo brand primitives from semantic UI tokens.
2. Make the Console consume semantic roles instead of raw brand colors.
3. Split the global stylesheet by tokens, foundations, shared components, and the
   observability feature.
4. Use the official Molejo logo assets and bundle the documented Space Grotesk and
   IBM Plex type families locally.
5. Correct text and control contrast, checkbox presentation, focus behavior, and
   static-versus-live alert semantics.
6. Add focused unit and integration guardrails for the design contract.

## Non-goals

- No white-label configuration, tenant theme editor, or runtime theme provider.
- No dark theme.
- No Storybook, visual-regression service, Tailwind migration, or component-library
  dependency.
- No redesign of product flows or unrelated feature refactoring.
- No shared design-token package or DTCG generation pipeline in this iteration.

## Acceptance criteria

- Authored colors are centralized in the theme contract, with documented exceptions
  only where required by HTML metadata.
- Shared UI and feature styles consume semantic tokens rather than Molejo brand
  primitives.
- Primary action text reaches WCAG AA contrast and visible control boundaries reach
  3:1 against their adjacent surfaces.
- Text inputs, selects, textareas, and checkboxes have type-appropriate presentation.
- The Console loads the official brand assets and font locally without a CDN.
- Persistent informational content is not exposed as an unsolicited live region.
- Design-token unit tests, shared-component integration tests, the complete Console
  test suite, and the production build pass.
