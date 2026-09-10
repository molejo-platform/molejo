# ADR-0003: Principals, authentication, and authorization

## Status

Proposed for v0.1.0.

## Date

2026-09-10

## Context

Humans, delivery automations, and internal services act on the same product
resources through different authentication mechanisms. Binding authorization to
passwords, sessions, email addresses, or one identity provider would make
resource ownership unstable and provider-specific.

## Decision

The Control Plane authorizes a durable Molejo `Principal`, independently of how
that principal authenticated. Principal kinds include human users, service
accounts, and internal system actors. Each kind has its own credential and
lifecycle rules.

Human identity, profile, installation role, Workspace membership, resource
relations, and audit history belong to Molejo. Authentication methods prove a
principal identity but do not own product authorization. Mutable display names,
usernames, and email addresses are attributes rather than authorization keys.

Service accounts are first-class non-human principals. Their credentials are
stored only as non-reversible verifiers, expire, can be rotated or revoked, and
are scoped through atomic permissions and resource ancestry.

Authorization is enforced by the Control Plane for every mutation and sensitive
read. UI state, Feature Availability, cluster consent, and possession of Agent
credentials never grant actor permissions. Authentication adapters cannot call
the Cluster Agent or bypass the same application use cases used by humans.

Local credentials are the initial authentication mechanism. Federation,
provisioning, account linking, and external group synchronization require
separate concrete contracts before they become supported behavior.

## Consequences

- Users and audit history survive a change in authentication mechanism.
- Human and automation access share resource policy without sharing credentials.
- External identity providers can be added without redefining Workspace or App
  ownership.
- Credential recovery and rotation cannot silently change product authorization.

## Alternatives considered

Using email or username as the external identity key was rejected because both
can change or collide. Delegating all authorization to an identity provider was
rejected because it couples product policy to that provider. Treating automation
tokens as user sessions was rejected because their ownership, scope, rotation,
and audit requirements differ.

## References

- [Security threat model](../architecture/security-threat-model.md)
- [ADR-0002: Product resource model and application delivery loop](0002-product-resource-model-and-application-delivery-loop.md)
