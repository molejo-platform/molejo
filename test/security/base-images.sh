#!/usr/bin/env bash

set -euo pipefail

for command in docker; do
  command -v "${command}" >/dev/null || {
    echo "required command not found: ${command}" >&2
    exit 1
  }
done
docker scout version >/dev/null

readonly BUILDER_IMAGE="fruto-vite-react-spa-builder:audit-$$"
readonly STATIC_IMAGE="fruto-static-html:audit-$$"
readonly SPA_IMAGE="fruto-vite-react-spa:audit-$$"

finish() {
  local exit_code=$?
  trap - EXIT
  docker image rm "${BUILDER_IMAGE}" "${STATIC_IMAGE}" "${SPA_IMAGE}" >/dev/null 2>&1 || true
  exit "${exit_code}"
}
trap finish EXIT

docker buildx build \
  --file test/fixtures/vite-react-spa/Dockerfile \
  --target build \
  --tag "${BUILDER_IMAGE}" \
  --load \
  .
docker buildx build \
  --file test/fixtures/static-html/Dockerfile \
  --tag "${STATIC_IMAGE}" \
  --load \
  .
docker buildx build \
  --file test/fixtures/vite-react-spa/Dockerfile \
  --tag "${SPA_IMAGE}" \
  --load \
  .

# The discarded builder is gated on its operating-system and JavaScript supply chain.
docker scout cves \
  --exit-code \
  --only-package-type apk,npm \
  --only-severity critical,high \
  "local://${BUILDER_IMAGE}"

for image in "${STATIC_IMAGE}" "${SPA_IMAGE}"; do
  docker scout cves \
    --exit-code \
    --only-severity critical,high \
    "local://${image}"
done
