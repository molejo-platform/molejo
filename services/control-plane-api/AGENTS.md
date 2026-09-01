# Automated tests
- Use a Testing Honeycomb: concentrate coverage on controlled service integrations.
- Test pure domain decisions with fast table-driven or fuzz tests and no external effects.
- Use real PostgreSQL for transactions, constraints, migrations, concurrency, leases, and idempotency.
- Test HTTP handlers with the real domain/store boundary; mock only systems outside the process.
- Test the build worker as a state machine, including timeout, retry, fencing, crash, and sanitization.
- Use controlled HTTP servers for GitHub; reserve the real GitHub App for an external canary.
- Judge test need by business risk, ownership, failure recovery, and an unproven contract boundary.
- Prove each behavior at the lowest layer that can detect it without duplicating assertions above.
- Add broad E2E only when the behavior uniquely crosses API, database, worker, or Kubernetes processes.
