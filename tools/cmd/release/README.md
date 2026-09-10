# Molejo release tool

The local release tool is the single entry point used to validate, build, publish, and verify a Molejo pre-release. Run it from the repository root:

```sh
VERSION=0.1.0-alpha.3
go -C tools run ./cmd/release check --version "$VERSION"
go -C tools run ./cmd/release build --version "$VERSION"
go -C tools run ./cmd/release publish --version "$VERSION"
go -C tools run ./cmd/release verify --version "$VERSION"
```

The commands have distinct boundaries:

- `check` validates the version, branch, clean worktree, required tools, repository,
  and tag state without publishing anything;
- `build` runs the repository gates and creates the complete local distribution;
- `publish` repeats the gates and build, pushes the four Linux/AMD64 images and two
  OCI Helm charts to GHCR, pushes the Git tag, and publishes the GitHub prerelease;
- `verify` compares the local distribution with the GitHub assets and performs
  anonymous pulls of every published GHCR image and chart.

Before `publish`, push the release branch and confirm its upstream points to the
exact local commit. The repository must be public, `gh` must be authenticated, and
Docker and Helm must be able to authenticate to GHCR. Publication is intentionally
local and operator-driven; GitHub Actions currently runs only deterministic
verification for `main` and `release/**` branches.

`publish` is safe to rerun for the same commit: immutable inputs are rebuilt and
release assets are replaced. It is not a dry run: it writes packages, a Git tag,
and a public GitHub prerelease.

`verify` checks local checksums, the GitHub prerelease and its assets, and anonymous pulls of every GHCR image and chart. Release output is written below `.release-dist/` and is not committed.
