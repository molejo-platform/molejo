#!/usr/bin/env bash
set -euo pipefail

for command in git kubectl sed install dirname mkdir mktemp grep; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

output="${FRUTO_OBSERVABILITY_RELEASE_OUTPUT:?set FRUTO_OBSERVABILITY_RELEASE_OUTPUT outside the checkout}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR outside the checkout}"
case "$output" in
  "$PWD"/*) echo "release manifests must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_OBSERVABILITY_RELEASE_OUTPUT must be absolute" >&2; exit 2;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"
release_metadata_load "$release_dir/metadata/observability.env"
[[ "$MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET" =~ ^molejo-observability-credentials-[a-f0-9]{12}$ ]]

git diff --quiet
git diff --cached --quiet
[[ -z "$(git ls-files --others --exclude-standard)" ]] || { echo "release rendering requires a clean checkout" >&2; exit 1; }

temporary="$(mktemp)"
trap 'rm -f "$temporary"' EXIT
commit="$(git rev-parse HEAD)"
{
  printf '# Molejo pre-alpha observability release\n'
  printf '# source_commit=%s architecture=linux/amd64 credentials=%s\n' "$commit" "$MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET"
  kubectl --context fruto-lab kustomize deploy/observability-lab |
    sed "s|required-external-observability-credentials-secret|$MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET|g"
} >"$temporary"
! grep -q 'required-external-' "$temporary" || { echo "observability release contains placeholders" >&2; exit 1; }
mkdir -p "$(dirname "$output")"
install -m 0644 "$temporary" "$output"
printf 'rendered %s from commit %s\n' "$output" "$commit"
