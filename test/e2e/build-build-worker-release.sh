#!/usr/bin/env bash
set -euo pipefail

for command in docker git install jq mkdir mktemp; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR to a directory outside the checkout}"
registry="${MOLEJO_BUILD_WORKER_REGISTRY:-registry.apps.calouro.tech}"
case "$release_dir" in
  "$PWD"/*) echo "release artifacts must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_DIR must be an absolute path" >&2; exit 2;;
esac

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

tag="$registry/build-worker:$commit"
docker buildx build --platform linux/amd64 --file services/control-plane-api/Dockerfile.build-worker --tag "$tag" --push --metadata-file "$temporary/build-worker.json" .
digest="$(jq -er '."containerimage.digest"' "$temporary/build-worker.json")"
image="${tag%:*}@$digest"

images_file="$temporary/builds.env"
{
  printf 'FRUTO_SOURCE_COMMIT=%q\n' "$commit"
  printf 'MOLEJO_BUILD_WORKER_IMAGE=%q\n' "$image"
} >"$images_file"
install -m 0600 "$images_file" "$release_dir/builds.env"
printf 'published linux/amd64 build worker metadata at %s\n' "$release_dir/builds.env"
