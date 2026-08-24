#!/usr/bin/env bash
set -euo pipefail

for command in docker git install jq mkdir mktemp; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR to a directory outside the checkout}"
testkit_image="${FRUTO_TESTKIT_IMAGE:?set FRUTO_TESTKIT_IMAGE}"
registry="${FRUTO_REGISTRY:-registry.apps.calouro.tech}"
case "$release_dir" in
  "$PWD"/*) echo "release artifacts must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_DIR must be an absolute path" >&2; exit 2;;
esac
[[ "$testkit_image" =~ ^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$ ]] || {
  echo "FRUTO_TESTKIT_IMAGE must be an immutable digest reference" >&2
  exit 2
}

git diff --quiet
git diff --cached --quiet
[[ -z "$(git ls-files --others --exclude-standard)" ]] || {
  echo "release builds require a clean checkout" >&2
  exit 1
}

commit="$(git rev-parse HEAD)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
mkdir -p "$release_dir"

api_tag="$registry/control-plane-api:$commit"
console_tag="$registry/console-web:$commit"
docker buildx build --platform linux/amd64 --file services/control-plane-api/Dockerfile --tag "$api_tag" --push --metadata-file "$temporary/api.json" .
docker buildx build --platform linux/amd64 --file apps/console-web/Dockerfile --tag "$console_tag" --push --metadata-file "$temporary/console.json" .

api_digest="$(jq -er '."containerimage.digest"' "$temporary/api.json")"
console_digest="$(jq -er '."containerimage.digest"' "$temporary/console.json")"
api_image="${api_tag%:*}@$api_digest"
console_image="${console_tag%:*}@$console_digest"

images_file="$temporary/images.env"
{
  printf 'FRUTO_SOURCE_COMMIT=%q\n' "$commit"
  printf 'FRUTO_API_IMAGE=%q\n' "$api_image"
  printf 'FRUTO_CONSOLE_IMAGE=%q\n' "$console_image"
  printf 'FRUTO_TESTKIT_IMAGE=%q\n' "$testkit_image"
} >"$images_file"
install -m 0600 "$images_file" "$release_dir/images.env"
printf 'published linux/amd64 release metadata at %s\n' "$release_dir/images.env"
