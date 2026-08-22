#!/usr/bin/env bash

set -euo pipefail

: "${OPERATOR_IMAGE:?OPERATOR_IMAGE must identify the locally built operator image}"

container_id=""
contents_file="$(mktemp)"

finish() {
  if [[ -n ${container_id} ]]; then
    docker rm --force "${container_id}" >/dev/null 2>&1 || true
  fi
  rm -f "${contents_file}"
}
trap finish EXIT

container_id="$(docker create "${OPERATOR_IMAGE}")"
docker export "${container_id}" | tar -tf - >"${contents_file}"

if ! grep -Eq '^(\./)?(etc/ssl/certs/ca-certificates\.crt|etc/pki/tls/certs/ca-bundle\.crt|etc/ssl/ca-bundle\.pem|etc/pki/tls/cacert\.pem)$' "${contents_file}"; then
  echo "operator image does not contain a standard system CA bundle required for OTLP over TLS" >&2
  exit 1
fi
