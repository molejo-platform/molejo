#!/usr/bin/env bash
set -euo pipefail

for command in git kubectl sed install dirname mkdir mktemp; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

output="${FRUTO_RELEASE_OUTPUT:?set FRUTO_RELEASE_OUTPUT to a path outside the checkout}"
case "$output" in
  "$PWD"/*) echo "release manifests must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_OUTPUT must be an absolute path" >&2; exit 2;;
esac

api_image="${FRUTO_API_IMAGE:?set FRUTO_API_IMAGE}"
console_image="${FRUTO_CONSOLE_IMAGE:?set FRUTO_CONSOLE_IMAGE}"
testkit_image="${FRUTO_TESTKIT_IMAGE:?set FRUTO_TESTKIT_IMAGE}"
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
  printf '# Molejo pre-alpha control-plane release\n'
  printf '# source_commit=%s architecture=linux/amd64 api=%s console=%s testkit=%s\n' "$commit" "$api_image" "$console_image" "$testkit_image"
  kubectl kustomize deploy/control-plane-lab |
    sed -e "s|ghcr.io/fruto-platform/control-plane-api@sha256:0000000000000000000000000000000000000000000000000000000000000000|$api_image|g" \
      -e "s|ghcr.io/fruto-platform/console-web@sha256:0000000000000000000000000000000000000000000000000000000000000000|$console_image|g" \
      -e "s|required-external-cluster-uid|$cluster_uid|g" \
      -e "s|required-external-proxy-cidr|$proxy_cidr|g"
} >"$temporary"

if grep -Eq 'sha256:0{64}|required-external-' "$temporary"; then
  echo "release manifest still contains a placeholder" >&2
  exit 1
fi
mkdir -p "$(dirname "$output")"
install -m 0644 "$temporary" "$output"
printf 'rendered %s from commit %s\n' "$output" "$commit"
