# Automated tests
- Use a controller testing ladder: pure decisions, envtest integration, then minimal Kind acceptance.
- Test deterministic rendering, naming, validation, and condition decisions with focused Go tests.
- Use envtest as the default proof for reconciliation, CRDs, status, ownership, and idempotency.
- Use Kind only for behavior requiring real scheduling, rollout, probes, networking, or garbage collection.
- Judge test need by desired-state risk, retry behavior, ownership, drift, and recovery after interruption.
- Cover success, failure, retry, concurrency, deletion, orphan prevention, and status sanitization as relevant.
- Prefer eventual assertions driven by observed state; do not use fixed sleeps as synchronization.
- When Kind finds controller logic failure, add the smallest envtest or unit regression that proves it.
- Keep system E2E limited to critical AppDeployment-to-public-workload lifecycle contracts.
