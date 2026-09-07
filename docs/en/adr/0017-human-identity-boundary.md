# ADR 0017: Human identity boundary

## Status

Accepted for the alpha architecture.

## Decision

Molejo separates the durable human account from authentication methods. A
`user` owns its profile, lifecycle status, installation roles, Workspace
memberships, and audit history. Local password credentials, MFA credentials,
and future external identities authenticate that user but do not own its
authorization.

Future OIDC or SAML support must map the provider's immutable `(issuer,
subject)` pair to a Molejo user. Email, display name, and username are mutable
attributes and must not be used as the external identity key. Provider groups
must be translated explicitly into Molejo roles or relations; they do not
bypass the authorization policy.

For the alpha, invitations create a user in `Invited` state without a password.
Accepting an expiring, single-use invitation creates the local credential and
activates the same user. Credential changes invalidate sessions through the
user authentication version.

## Consequences

Workspaces and audit records remain stable if authentication providers change.
Adding an external provider requires a new identity adapter and mapping store,
not changes to membership or application-domain contracts. Account linking and
provider-driven provisioning remain explicit future decisions.

## Alternatives Considered

Using username or email as the provider identity was rejected because either
attribute can change or collide. Storing authorization only in the identity
provider was rejected because it would couple Molejo application policy to one
provider.

## References

- [ADR 0015: Capability ownership](0015-capability-ownership.md)
- [User management foundation plan](../../plans/2026-09-06-user-management-foundation/MANIFESTO.md)
