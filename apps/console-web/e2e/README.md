# Console browser acceptance

Playwright owns the browser contract. Environment provisioning, publication
bindings, domains, grants, DNS, Gateway, TLS, and disposable cluster cleanup
remain outside the browser suite.

The basic journey requires a running Console and the bootstrap owner password:

```bash
MOLEJO_E2E_BASE_URL=https://console.example.test \
MOLEJO_E2E_PASSWORD='<owner password>' \
corepack pnpm --filter @molejo-platform/console-web e2e
```

The HTTP publication journey additionally requires an Exact domain, a
SubdomainPool, grants for the signed-in Workspace and cluster, and an immutable
test image available to that cluster:

```bash
MOLEJO_E2E_BASE_URL=https://console.example.test \
MOLEJO_E2E_PASSWORD='<owner password>' \
MOLEJO_E2E_EXACT_HOST=example.test \
MOLEJO_E2E_POOL_DOMAIN=apps.example.test \
MOLEJO_E2E_POOL_LABEL=browser \
MOLEJO_E2E_IMAGE='registry.example.test/testkit@sha256:...' \
corepack pnpm --filter @molejo-platform/console-web e2e
```

Use `molejoctl capability publication plan`, `apply`, and `verify` to prepare
the operator-owned resources. The browser test only performs the developer
journey: select granted destinations, save, deploy, observe, and remove one
association. Results are written to `test-results/playwright.json`; traces and
screenshots are retained only on failure.
