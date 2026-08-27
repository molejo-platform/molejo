# Automated tests
- Use the Testing Trophy: static checks and page integration tests provide the main coverage.
- Test pure functions with focused Vitest unit tests.
- Prefer Vitest and Testing Library for forms, states, permissions, navigation, and API payloads.
- Judge test need by user-visible behavior, regression risk, and confidence not already provided below.
- Assert accessible roles and visible outcomes; avoid CSS selectors and implementation details.
- Add Playwright only when a real browser or the complete frontend/backend wiring is required.
- Keep Playwright limited to isolated critical journeys; do not repeat edge-case matrices there.
- When Playwright finds a logic bug, add the smallest lower-level regression test that proves it.
- Run the smallest relevant suite first and reserve browser acceptance for affected high-risk flows.
