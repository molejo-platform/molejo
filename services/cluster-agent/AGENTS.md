# Automated tests
- Test state and identity decisions with fast table-driven tests.
- Use fake Kubernetes clients only for exact Secret persistence behavior.
- Use real TLS and in-memory gRPC for trust-boundary contracts.
- Use Kind only for ServiceAccount, RBAC, probes, and process integration.
- Prove crash retry, idempotent enrollment, reconnect, and sanitization.
- Never place tokens, private keys, certificates, or kubeconfigs in snapshots.
- Prefer eventual assertions over fixed sleeps.
- Add broad E2E only when behavior crosses Agent, API, database, and Kubernetes.
