# Contributing

## Prerequisites

- Go 1.26.6, the version used by CI.
- `just` 1.57 or newer.
- Docker for PostgreSQL integration tests.
- `kubectl` only for acceptance checks against a real cluster.

The first test run downloads Go modules and the Kubernetes `envtest` binaries.
Use `just --list` to discover the maintained commands.

## Tests

Run the fast test suite without external services:

```bash
just test
```

PostgreSQL integration tests require a running Docker daemon. Testcontainers
starts PostgreSQL 17.6 on a random host port and removes it after each package
suite:

```bash
just integration-test
```

To use an existing PostgreSQL instance instead of Docker, provide its URL:

```bash
MOLEJO_TEST_DATABASE_URL='postgres://user:password@host/database?sslmode=disable' just integration-test
```

`just verify` runs both suites and all repository quality gates.

## K3s acceptance checks

These maintainer checks are not part of `just verify` because they require an
existing cluster:

```bash
tools/testing/control-plane-k3s.sh verify --context molejo-k3s
tools/testing/tls-k3s.sh verify --context molejo-k3s --profile default
```

`teardown` and `cycle` change cluster state and require an explicit
`--confirm <context>` argument. Run the scripts without arguments to see their
complete usage.
