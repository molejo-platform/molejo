# Molejo

> Experimental alpha project. Molejo is not ready for production.

Molejo is a public and portable Kubernetes Application Platform. It aims
to let people create, publish, and operate applications without requiring them to
understand Kubernetes, `kubectl`, YAML, or the underlying infrastructure.

This repository is the public monorepo for the product. Kubernetes is its execution
substrate, while Molejo contracts and APIs represent the product intent exposed to
users.

## Documentation

Choose a language:

- [English](docs/en/README.md)
- [Português (Brasil)](docs/pt-BR/README.md)
- [Español (Argentina)](docs/es-AR/README.md)

English is the canonical source. Localized documentation mirrors the same relative
structure whenever an equivalent page is available.

Current experimental components:

- [Operational model](docs/en/architecture/operational-model.md)
- [Platform lifecycle](docs/en/platform/lifecycle.md)
- [Platform Operator](docs/en/platform/platform-operator.md)
- control-plane API and Console;
- [Outbound Cluster Agent](docs/en/platform/cluster-agent.md)
- [Cluster capabilities](docs/en/capabilities/README.md)
- [Application delivery loop](docs/en/application-loop/README.md)

## Try the Alpha

Start with the published [GitHub prereleases](https://github.com/molejo-platform/molejo/releases)
and follow the [platform lifecycle](docs/en/platform/lifecycle.md). Alpha releases
may change contracts without compatibility or migration guarantees.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, validation, and the
language-specific contribution guides.

## Community

- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security Policy](SECURITY.md)
- [Support](SUPPORT.md)
- [Maintainers](MAINTAINERS.md)
- [Apache License 2.0](LICENSE)
