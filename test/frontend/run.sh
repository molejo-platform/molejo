#!/usr/bin/env bash

set -euo pipefail

readonly STATIC_IMAGE="fruto-static-html:test-$$"
readonly SPA_IMAGE="fruto-vite-react-spa:test-$$"
readonly STATIC_BODY="$(mktemp)"
readonly STATIC_HEADERS="$(mktemp)"
readonly SPA_BODY="$(mktemp)"
readonly SPA_HEADERS="$(mktemp)"

STATIC_CONTAINER=""
SPA_CONTAINER=""
REGRESSION_FAILURES=0

finish() {
  local exit_code=$?
  trap - EXIT

  if [[ -n ${STATIC_CONTAINER} ]]; then
    docker rm --force "${STATIC_CONTAINER}" >/dev/null 2>&1 || true
  fi
  if [[ -n ${SPA_CONTAINER} ]]; then
    docker rm --force "${SPA_CONTAINER}" >/dev/null 2>&1 || true
  fi
  docker image rm "${STATIC_IMAGE}" "${SPA_IMAGE}" >/dev/null 2>&1 || true
  rm -f "${STATIC_BODY}" "${STATIC_HEADERS}" "${SPA_BODY}" "${SPA_HEADERS}"
  exit "${exit_code}"
}
trap finish EXIT

wait_for_http() {
  local url=$1
  for _ in $(seq 1 50); do
    if curl --fail --silent --show-error "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  echo "timed out waiting for ${url}" >&2
  return 1
}

mapped_port() {
  local container=$1
  docker port "${container}" 8080/tcp | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | head -n 1
}

assert_status() {
  local expected=$1
  local url=$2
  local actual
  actual="$(curl --silent --output /dev/null --write-out '%{http_code}' "${url}")"
  if [[ ${actual} != "${expected}" ]]; then
    echo "expected ${url} to return HTTP ${expected}, got ${actual}" >&2
    return 1
  fi
}

assert_header() {
  local headers_file=$1
  local expected=$2
  if ! tr -d '\r' <"${headers_file}" | grep -Fqi "${expected}"; then
    echo "expected response headers to contain ${expected}" >&2
    cat "${headers_file}" >&2
    return 1
  fi
}

assert_header_absent() {
  local headers_file=$1
  local unexpected=$2
  if tr -d '\r' <"${headers_file}" | grep -Fqi "${unexpected}"; then
    echo "expected response headers not to contain ${unexpected}" >&2
    cat "${headers_file}" >&2
    return 1
  fi
}

check_regression() {
  if ! "$@"; then
    REGRESSION_FAILURES=$((REGRESSION_FAILURES + 1))
  fi
}

docker buildx build \
  --file test/fixtures/static-html/Dockerfile \
  --tag "${STATIC_IMAGE}" \
  --load \
  .
docker buildx build \
  --file test/fixtures/vite-react-spa/Dockerfile \
  --tag "${SPA_IMAGE}" \
  --build-arg APP_VERSION=v1 \
  --load \
  .

for image in "${STATIC_IMAGE}" "${SPA_IMAGE}"; do
  image_user="$(docker image inspect "${image}" --format '{{.Config.User}}')"
  if [[ ${image_user} != "65532:65532" ]]; then
    echo "expected ${image} to run as 65532:65532, got ${image_user:-root}" >&2
    exit 1
  fi
done

STATIC_CONTAINER="$(docker run --detach --read-only --cap-drop ALL \
  --security-opt no-new-privileges \
  --publish 127.0.0.1::8080 "${STATIC_IMAGE}")"
static_port="$(mapped_port "${STATIC_CONTAINER}")"
wait_for_http "http://127.0.0.1:${static_port}/healthz"

curl --silent --show-error --dump-header "${STATIC_HEADERS}" \
  --output "${STATIC_BODY}" "http://127.0.0.1:${static_port}/"
grep -Fq 'data-profile="static-html"' "${STATIC_BODY}"
assert_header "${STATIC_HEADERS}" 'Cache-Control: no-cache'
assert_status 200 "http://127.0.0.1:${static_port}/about.html"
assert_status 200 "http://127.0.0.1:${static_port}/readyz"
assert_status 404 "http://127.0.0.1:${static_port}/missing"
assert_status 404 "http://127.0.0.1:${static_port}/assets/missing.css"
check_regression assert_status 404 "http://127.0.0.1:${static_port}/50x.html"
curl --silent --show-error --dump-header "${STATIC_HEADERS}" \
  --output /dev/null "http://127.0.0.1:${static_port}/assets/missing.css"
check_regression assert_header_absent "${STATIC_HEADERS}" \
  'Cache-Control: public, max-age=31536000, immutable'
curl --fail --silent --show-error --dump-header "${STATIC_HEADERS}" \
  --output /dev/null "http://127.0.0.1:${static_port}/assets/styles-9a4b7c2d.css"
assert_header "${STATIC_HEADERS}" 'Cache-Control: public, max-age=31536000, immutable'

SPA_CONTAINER="$(docker run --detach --read-only --cap-drop ALL \
  --security-opt no-new-privileges \
  --publish 127.0.0.1::8080 "${SPA_IMAGE}")"
spa_port="$(mapped_port "${SPA_CONTAINER}")"
wait_for_http "http://127.0.0.1:${spa_port}/healthz"

curl --silent --show-error --dump-header "${SPA_HEADERS}" \
  --output "${SPA_BODY}" "http://127.0.0.1:${spa_port}/"
grep -Fq 'name="fruto-profile" content="vite-react-spa"' "${SPA_BODY}"
grep -Fq 'name="fruto-version" content="v1"' "${SPA_BODY}"
assert_header "${SPA_HEADERS}" 'Cache-Control: no-cache'
curl --fail --silent --show-error --output "${SPA_BODY}" \
  "http://127.0.0.1:${spa_port}/projects/example"
grep -Fq 'name="fruto-profile" content="vite-react-spa"' "${SPA_BODY}"
assert_status 200 "http://127.0.0.1:${spa_port}/readyz"
assert_status 404 "http://127.0.0.1:${spa_port}/assets/missing.js"
assert_status 404 "http://127.0.0.1:${spa_port}/missing.css"
check_regression assert_status 404 "http://127.0.0.1:${spa_port}/50x.html"
curl --silent --show-error --dump-header "${SPA_HEADERS}" \
  --output /dev/null "http://127.0.0.1:${spa_port}/assets/missing.js"
check_regression assert_header_absent "${SPA_HEADERS}" \
  'Cache-Control: public, max-age=31536000, immutable'

spa_asset="$(sed -n 's|.*src="\(/assets/[^\"]*\.js\)".*|\1|p' "${SPA_BODY}" | head -n 1)"
if [[ -z ${spa_asset} ]]; then
  echo "could not discover the fingerprinted SPA JavaScript asset" >&2
  exit 1
fi
curl --fail --silent --show-error --dump-header "${SPA_HEADERS}" \
  --output /dev/null "http://127.0.0.1:${spa_port}${spa_asset}"
assert_header "${SPA_HEADERS}" 'Cache-Control: public, max-age=31536000, immutable'

if ((REGRESSION_FAILURES > 0)); then
  echo "frontend image contracts found ${REGRESSION_FAILURES} regression(s)" >&2
  exit 1
fi

echo "frontend image contracts passed"
