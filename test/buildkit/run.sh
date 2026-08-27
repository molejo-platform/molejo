#!/usr/bin/env bash

set -euo pipefail

readonly BUILDKIT_IMAGE="moby/buildkit:v0.32.2-rootless@sha256:504731e577c20559c00f968f33219f30115e70be29ab96728d1d06e963fc494b"
readonly REGISTRY_IMAGE="registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
readonly SUFFIX="${PPID}-$$"
readonly NETWORK="molejo-buildkit-test-${SUFFIX}"
readonly BUILDKIT_CONTAINER="molejo-buildkitd-test-${SUFFIX}"
readonly REGISTRY_CONTAINER="molejo-registry-test-${SUFFIX}"
readonly TEMPORARY_DIRECTORY="$(mktemp -d)"

finish() {
  local exit_code=$?
  trap - EXIT
  docker rm --force "${BUILDKIT_CONTAINER}" "${REGISTRY_CONTAINER}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK}" >/dev/null 2>&1 || true
  rm -rf "${TEMPORARY_DIRECTORY}"
  exit "${exit_code}"
}
trap finish EXIT

cat >"${TEMPORARY_DIRECTORY}/buildkitd.toml" <<'EOF'
[registry."registry:5000"]
  http = true
  insecure = true
EOF

docker network create "${NETWORK}" >/dev/null
docker run --detach --name "${REGISTRY_CONTAINER}" --network "${NETWORK}" --network-alias registry \
  --publish 127.0.0.1::5000 "${REGISTRY_IMAGE}" >/dev/null
docker run --detach --name "${BUILDKIT_CONTAINER}" --network "${NETWORK}" --network-alias buildkitd \
  --privileged --volume "${TEMPORARY_DIRECTORY}/buildkitd.toml:/tmp/buildkitd.toml:ro" \
  "${BUILDKIT_IMAGE}" --config /tmp/buildkitd.toml --addr tcp://0.0.0.0:1234 --oci-worker-no-process-sandbox >/dev/null

registry_port="$(docker port "${REGISTRY_CONTAINER}" 5000/tcp | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | head -n 1)"
for _ in $(seq 1 60); do
  if curl --fail --silent "http://127.0.0.1:${registry_port}/v2/" >/dev/null 2>&1 && \
    docker run --rm --network "${NETWORK}" --entrypoint buildctl "${BUILDKIT_IMAGE}" \
      --addr tcp://buildkitd:1234 debug workers >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [[ ${ready:-false} != true ]]; then
  docker logs "${BUILDKIT_CONTAINER}" >&2
  exit 1
fi

FRUTO_TEST_BUILDKIT_NETWORK="${NETWORK}" \
FRUTO_TEST_BUILDKIT_CLIENT_IMAGE="${BUILDKIT_IMAGE}" \
FRUTO_TEST_REGISTRY_URL="http://127.0.0.1:${registry_port}" \
GOCACHE="${GOCACHE:-/tmp/fruto-go-cache}" \
go test -count=1 -run '^TestBuildKitSystemPushesAnImmutableImage$' ./services/control-plane-api/internal/build

echo "BuildKit system contract passed"
