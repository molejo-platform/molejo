# Molejo release tool

The local release tool is the single entry point used to validate, build, publish, and verify a Molejo pre-release. Run it from the repository root:

```sh
go -C tools run ./cmd/release check --version 0.1.0-alpha.1
go -C tools run ./cmd/release build --version 0.1.0-alpha.1
go -C tools run ./cmd/release publish --version 0.1.0-alpha.1
go -C tools run ./cmd/release verify --version 0.1.0-alpha.1
```

`publish` runs the repository gates, creates the `molejoctl` archives, publishes the four Linux/AMD64 images and two OCI Helm charts to GHCR, pushes the Git tag, and creates the GitHub prerelease. It is safe to rerun for the same commit: immutable inputs are rebuilt and release assets are replaced.

`verify` checks local checksums, the GitHub prerelease and its assets, and anonymous pulls of every GHCR image and chart. Release output is written below `.release-dist/` and is not committed.
