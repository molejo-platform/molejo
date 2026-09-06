# ADR 0016: Alpha lifecycle policy

## Status

Accepted for the alpha architecture.

## Decision

Alpha releases do not provide backward-compatible CLI paths, API schemas, CRD
conversion, or in-place upgrades. A release may replace an experimental contract
when that improves its long-term boundary. The supported transition between
different alpha installations is backup where relevant, teardown, and clean
reinstall.

## Consequences

The project can validate durable contracts before compatibility becomes a public
promise. Installers fail with an actionable clean-reinstall message when they find
a different alpha version. Resource ownership and secret-safety checks remain
mandatory even though compatibility is intentionally absent.
