#!/usr/bin/env bash
set -euo pipefail

for command in git kubectl sed install dirname mkdir mktemp cp; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

output="${FRUTO_RELEASE_OUTPUT:?set FRUTO_RELEASE_OUTPUT to a path outside the checkout}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR outside the checkout}"
case "$output" in
  "$PWD"/*) echo "release manifests must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_OUTPUT must be an absolute path" >&2; exit 2;;
esac

api_image="${FRUTO_API_IMAGE:?set FRUTO_API_IMAGE}"
console_image="${FRUTO_CONSOLE_IMAGE:?set FRUTO_CONSOLE_IMAGE}"
testkit_image="${FRUTO_TESTKIT_IMAGE:?set FRUTO_TESTKIT_IMAGE}"
build_image_repository="${MOLEJO_BUILD_IMAGE_REPOSITORY:?set MOLEJO_BUILD_IMAGE_REPOSITORY}"
cluster_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
proxy_cidr="${FRUTO_TRUSTED_PROXY_CIDR:?set FRUTO_TRUSTED_PROXY_CIDR}"
for reference in "$api_image" "$console_image" "$testkit_image"; do
  [[ "$reference" =~ ^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$ ]] || {
    echo "release images must be immutable digest references" >&2
    exit 2
  }
done
[[ -n "$cluster_uid" && "$cluster_uid" != required-* ]]
[[ "$proxy_cidr" =~ ^[^[:space:]]+/[0-9]{1,3}$ ]]
[[ "$build_image_repository" =~ ^[a-z0-9][a-z0-9._:/-]*$ && "$build_image_repository" == */* ]] || { echo "MOLEJO_BUILD_IMAGE_REPOSITORY must include an OCI registry and repository prefix" >&2; exit 2; }
build_registry="${build_image_repository%%/*}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"
release_metadata_load "$release_dir/metadata/control-plane.env"
release_metadata_load "$release_dir/metadata/openbao.env"
release_metadata_load "$release_dir/metadata/observability.env"
for name in "$MOLEJO_GITHUB_APP_SECRET" "$MOLEJO_GITHUB_WEBHOOK_SECRET" "$MOLEJO_PASSWORD_RESET_SECRET" "$MOLEJO_OPENBAO_CA_CONFIGMAP" "$MOLEJO_PARAMETER_FINGERPRINT_SECRET" "$MOLEJO_OBSERVABILITY_READER_SECRET"; do
  [[ "$name" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || { echo "invalid release object name" >&2; exit 2; }
done

git diff --quiet
git diff --cached --quiet
[[ -z "$(git ls-files --others --exclude-standard)" ]] || {
  echo "release rendering requires a clean checkout" >&2
  exit 1
}

temporary="$(mktemp)"
temporary_dir="$(mktemp -d)"
trap 'rm -f "$temporary"; rm -rf "$temporary_dir"' EXIT
cp -R deploy/control-plane "$temporary_dir/control-plane"
cp -R deploy/control-plane-lab "$temporary_dir/control-plane-lab"
sed \
  -e "s|required-external-cluster-uid|$cluster_uid|g" \
  -e "s|required-external-proxy-cidr|$proxy_cidr|g" \
  -e "s|required-external-build-registry|$build_registry|g" \
  deploy/control-plane/config.env >"$temporary_dir/control-plane/config.env"
commit="$(git rev-parse HEAD)"
{
  printf '# Molejo pre-alpha control-plane release\n'
  printf '# source_commit=%s architecture=linux/amd64 api=%s console=%s testkit=%s\n' "$commit" "$api_image" "$console_image" "$testkit_image"
  kubectl --context fruto-lab kustomize "$temporary_dir/control-plane-lab" |
    sed -e "s|ghcr.io/fruto-platform/control-plane-api@sha256:0000000000000000000000000000000000000000000000000000000000000000|$api_image|g" \
      -e "s|ghcr.io/fruto-platform/console-web@sha256:0000000000000000000000000000000000000000000000000000000000000000|$console_image|g" \
      -e "s|molejo-github-app|$MOLEJO_GITHUB_APP_SECRET|g" \
      -e "s|required-external-github-webhook-secret|$MOLEJO_GITHUB_WEBHOOK_SECRET|g" \
      -e "s|required-external-password-reset-secret|$MOLEJO_PASSWORD_RESET_SECRET|g" \
      -e "s|required-external-openbao-ca-configmap|$MOLEJO_OPENBAO_CA_CONFIGMAP|g" \
      -e "s|required-external-parameter-fingerprint-secret|$MOLEJO_PARAMETER_FINGERPRINT_SECRET|g" \
      -e "s|required-external-observability-reader-secret|$MOLEJO_OBSERVABILITY_READER_SECRET|g"
} >"$temporary"

if grep -Eq 'sha256:0{64}|required-external-' "$temporary"; then
  echo "release manifest still contains a placeholder" >&2
  exit 1
fi
mkdir -p "$(dirname "$output")"
install -m 0644 "$temporary" "$output"
printf 'rendered %s from commit %s\n' "$output" "$commit"
