#!/usr/bin/env bash
set -euo pipefail

for command in git kubectl sed install dirname mkdir mktemp; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

output="${FRUTO_BUILDS_RELEASE_OUTPUT:?set FRUTO_BUILDS_RELEASE_OUTPUT to a path outside the checkout}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR outside the checkout}"
case "$output" in
  "$PWD"/*) echo "release manifests must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_BUILDS_RELEASE_OUTPUT must be an absolute path" >&2; exit 2;;
esac

worker_image="${MOLEJO_BUILD_WORKER_IMAGE:?set MOLEJO_BUILD_WORKER_IMAGE}"
image_repository="${MOLEJO_BUILD_IMAGE_REPOSITORY:?set MOLEJO_BUILD_IMAGE_REPOSITORY}"
[[ "$worker_image" =~ ^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$ ]] || { echo "MOLEJO_BUILD_WORKER_IMAGE must be an immutable digest reference" >&2; exit 2; }
[[ "$image_repository" =~ ^[a-z0-9][a-z0-9._:/-]*$ && "$image_repository" != */ ]] || { echo "MOLEJO_BUILD_IMAGE_REPOSITORY must be an OCI repository prefix" >&2; exit 2; }
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"
release_metadata_load "$release_dir/metadata/builds.env"
for name in "$MOLEJO_BUILD_WORKER_SECRET" "$MOLEJO_BUILDKIT_TLS_SECRET" "$MOLEJO_BUILD_REGISTRY_SECRET"; do
  [[ "$name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || { echo "invalid build release object name" >&2; exit 2; }
done

git diff --quiet
git diff --cached --quiet
[[ -z "$(git ls-files --others --exclude-standard)" ]] || {
  echo "release rendering requires a clean checkout" >&2
  exit 1
}

temporary="$(mktemp)"
trap 'rm -f "$temporary"' EXIT
commit="$(git rev-parse HEAD)"
{
  printf '# Molejo pre-alpha build-plane release\n'
  printf '# source_commit=%s architecture=linux/amd64 worker=%s\n' "$commit" "$worker_image"
  kubectl --context fruto-lab kustomize deploy/builds |
    sed -e "s|ghcr.io/molejo-platform/build-worker@sha256:0000000000000000000000000000000000000000000000000000000000000000|$worker_image|g" \
      -e "s|value: CHANGE_ME|value: $image_repository|g" \
      -e "s|required-external-build-worker-secret|$MOLEJO_BUILD_WORKER_SECRET|g" \
      -e "s|required-external-buildkit-tls-secret|$MOLEJO_BUILDKIT_TLS_SECRET|g" \
      -e "s|required-external-build-registry-secret|$MOLEJO_BUILD_REGISTRY_SECRET|g"
} >"$temporary"

if grep -Eq 'sha256:0{64}|CHANGE_ME|required-external-' "$temporary"; then
  echo "build release manifest still contains a placeholder" >&2
  exit 1
fi
mkdir -p "$(dirname "$output")"
install -m 0644 "$temporary" "$output"
printf 'rendered %s from commit %s\n' "$output" "$commit"
