# External CI Releases

External CI builds and pushes an image; Molejo registers and deploys the
immutable result. The integration never patches Kubernetes directly.

## Create an automation credential

A Workspace Owner creates an App service account through
`POST /api/v1/workspaces/{workspaceId}/projects/{projectId}/apps/{appId}/service-accounts`.
The request chooses the App Environments that may receive deployments. Issue a
credential with `POST .../service-accounts/{serviceAccountId}/tokens`. That
response exposes the bearer token once; store it as a masked CI secret and never
commit it. Rotation issues a second token for the same identity, updates the CI
secret, and revokes the previous token with `DELETE .../tokens/{tokenId}`.

Source and producer fields are declarations made by the authenticated pipeline,
not cryptographic attestations. Releases expose this explicitly as
`provenanceStatus=Declared`.

## Pipeline contract

1. Build and push the OCI image.
2. Resolve its digest and register `repository@sha256:digest` with a stable
   `Idempotency-Key` for that CI run.
3. Read the target App Environment to obtain `version`,
   `configurationVersion`, and `currentDeploymentId`.
4. Submit the deployment with a different idempotency key and `If-Match` equal
   to the environment version.
5. Optionally poll the returned Operation. A version conflict means another
   actor changed desired state; read again and make an explicit retry decision.

The control plane rejects tagged images and registries outside
`MOLEJO_ALLOWED_REGISTRIES`. Kubernetes image-pull credentials remain a Day Zero
cluster configuration; they are not carried by this API.

A complete GitHub Actions reference is available at
[`docs/examples/github-actions-external-release.yml`](../../examples/github-actions-external-release.yml).
