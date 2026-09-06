# Contributing

Contribution guidelines will be added soon.

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
