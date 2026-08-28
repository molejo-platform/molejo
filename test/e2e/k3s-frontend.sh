#!/usr/bin/env bash

set -euo pipefail

readonly KUBE_CONTEXT="${FRUTO_KUBE_CONTEXT:-fruto-lab}"
readonly REGISTRY="${FRUTO_REGISTRY:-registry.apps.calouro.tech}"
readonly REGISTRY_SECRET_NAMESPACE="${FRUTO_REGISTRY_SECRET_NAMESPACE:-fruto-system}"
readonly REGISTRY_SECRET_NAME="${FRUTO_REGISTRY_SECRET_NAME:-registry-pull}"
readonly STATIC_NAMESPACE="ws-e2e-static"
readonly SPA_NAMESPACE="ws-e2e-spa"
readonly STATIC_APP="ap-static000001"
readonly SPA_APP="ap-spa000001"
readonly STATIC_HOSTNAME="static.molejo.dev"
readonly SPA_HOSTNAME="spa.molejo.dev"
readonly STATIC_REPOSITORY="${REGISTRY}/fruto-static-html"
readonly SPA_REPOSITORY="${REGISTRY}/fruto-vite-react-spa"
readonly RELEASE_ID="$(date -u +%Y%m%d%H%M%S)-$$"
readonly STATIC_METADATA="$(mktemp)"
readonly SPA_V1_METADATA="$(mktemp)"
readonly SPA_V2_METADATA="$(mktemp)"
readonly STATIC_MANIFEST="$(mktemp)"
readonly SPA_MANIFEST="$(mktemp)"
readonly RESPONSE_BODY="$(mktemp)"
readonly RESPONSE_HEADERS="$(mktemp)"

kube() {
  kubectl --context "${KUBE_CONTEXT}" "$@"
}

diagnostics() {
  kube get appdeployments,deployments,services,httproutes \
    -n "${STATIC_NAMESPACE}" -o wide || true
  kube get appdeployments,deployments,services,httproutes \
    -n "${SPA_NAMESPACE}" -o wide || true
  kube get events -n "${STATIC_NAMESPACE}" --sort-by=.metadata.creationTimestamp || true
  kube get events -n "${SPA_NAMESPACE}" --sort-by=.metadata.creationTimestamp || true
}

finish() {
  local exit_code=$?
  trap - EXIT
  if [[ ${exit_code} -ne 0 ]]; then
    diagnostics
  fi
  rm -f \
    "${STATIC_METADATA}" \
    "${SPA_V1_METADATA}" \
    "${SPA_V2_METADATA}" \
    "${STATIC_MANIFEST}" \
    "${SPA_MANIFEST}" \
    "${RESPONSE_BODY}" \
    "${RESPONSE_HEADERS}"
  exit "${exit_code}"
}
trap finish EXIT

for command in curl docker jq kubectl sed; do
  command -v "${command}" >/dev/null || {
    echo "required command not found: ${command}" >&2
    exit 1
  }
done

if ! kubectl config get-contexts "${KUBE_CONTEXT}" --no-headers >/dev/null 2>&1; then
  echo "Kubernetes context not found: ${KUBE_CONTEXT}" >&2
  exit 1
fi
if [[ ${KUBE_CONTEXT} != "fruto-lab" && ${FRUTO_ALLOW_CUSTOM_CONTEXT:-false} != "true" ]]; then
  echo "refusing non-fruto context ${KUBE_CONTEXT}; set FRUTO_ALLOW_CUSTOM_CONTEXT=true to override" >&2
  exit 1
fi

kube get --raw=/readyz >/dev/null
node_architectures="$(kube get nodes -o jsonpath='{range .items[*]}{.status.nodeInfo.architecture}{"\n"}{end}' | sort -u)"
if [[ ${node_architectures} != "amd64" ]]; then
  echo "frontend publication expects an amd64-only cluster, got: ${node_architectures}" >&2
  exit 1
fi
kube wait --for=condition=Programmed gateway/fruto -n fruto-system --timeout=60s
kube get secret "${REGISTRY_SECRET_NAME}" \
  -n "${REGISTRY_SECRET_NAMESPACE}" >/dev/null
docker buildx inspect >/dev/null

build_and_push() {
  local dockerfile=$1
  local image_tag=$2
  local metadata_file=$3
  shift 3

  docker buildx build \
    --platform linux/amd64 \
    --file "${dockerfile}" \
    --tag "${image_tag}" \
    --metadata-file "${metadata_file}" \
    --push \
    "$@" \
    . >&2

  local digest
  digest="$(jq -r '."containerimage.digest" // empty' "${metadata_file}")"
  if [[ ! ${digest} =~ ^sha256:[a-f0-9]{64}$ ]]; then
    echo "could not resolve the pushed digest for ${image_tag}" >&2
    return 1
  fi
  printf '%s' "${digest}"
}

static_digest="$(build_and_push \
  test/fixtures/static-html/Dockerfile \
  "${STATIC_REPOSITORY}:${RELEASE_ID}" \
  "${STATIC_METADATA}")"
spa_v1_digest="$(build_and_push \
  test/fixtures/vite-react-spa/Dockerfile \
  "${SPA_REPOSITORY}:${RELEASE_ID}-v1" \
  "${SPA_V1_METADATA}" \
  --build-arg APP_VERSION=v1)"
spa_v2_digest="$(build_and_push \
  test/fixtures/vite-react-spa/Dockerfile \
  "${SPA_REPOSITORY}:${RELEASE_ID}-v2" \
  "${SPA_V2_METADATA}" \
  --build-arg APP_VERSION=v2)"

readonly STATIC_IMAGE="${STATIC_REPOSITORY}@${static_digest}"
readonly SPA_IMAGE_V1="${SPA_REPOSITORY}@${spa_v1_digest}"
readonly SPA_IMAGE_V2="${SPA_REPOSITORY}@${spa_v2_digest}"

prepare_namespace() {
  local namespace=$1

  kube create namespace "${namespace}" --dry-run=client -o yaml | kube apply -f - >/dev/null
  kube get secret "${REGISTRY_SECRET_NAME}" \
    -n "${REGISTRY_SECRET_NAMESPACE}" -o json |
    jq --arg namespace "${namespace}" '
      .metadata.namespace = $namespace
      | del(
          .metadata.creationTimestamp,
          .metadata.managedFields,
          .metadata.resourceVersion,
          .metadata.uid
        )
    ' |
    kube apply -f - >/dev/null
  for _ in $(seq 1 30); do
    if kube get serviceaccount default -n "${namespace}" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
  kube patch serviceaccount default \
    -n "${namespace}" \
    --type=merge \
    --patch "{\"imagePullSecrets\":[{\"name\":\"${REGISTRY_SECRET_NAME}\"}]}" >/dev/null
}

prepare_namespace "${STATIC_NAMESPACE}"
prepare_namespace "${SPA_NAMESPACE}"

sed \
  -e "s|ws-static-e2e|${STATIC_NAMESPACE}|" \
  -e "s|__STATIC_IMAGE__|${STATIC_IMAGE}|" \
  test/e2e/static-appdeployment.yaml >"${STATIC_MANIFEST}"
sed \
  -e "s|ws-spa-e2e|${SPA_NAMESPACE}|" \
  -e "s|__SPA_IMAGE__|${SPA_IMAGE_V1}|" \
  -e 's|slug: spa-e2e|slug: spa|' \
  test/e2e/spa-appdeployment.yaml >"${SPA_MANIFEST}"

kube apply -f "${STATIC_MANIFEST}"
static_generation="$(kube get "appdeployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" -o jsonpath='{.metadata.generation}')"
kube wait --for=jsonpath='{.status.observedGeneration}'="${static_generation}" \
  "appdeployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=180s
kube wait --for=condition=Ready \
  "appdeployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=180s
kube rollout status \
  "deployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=180s
if kube get "httproute/${STATIC_APP}" -n "${STATIC_NAMESPACE}" >/dev/null 2>&1; then
  echo "static frontend unexpectedly started with public exposure" >&2
  exit 1
fi

kube patch "appdeployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --type=merge \
  --patch '{"spec":{"exposure":"Public","slug":"static"}}'
kube wait --for=jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'=True \
  "httproute/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=120s
kube wait --for=jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}'=True \
  "httproute/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=120s
static_public_generation="$(kube get "appdeployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" -o jsonpath='{.metadata.generation}')"
kube wait --for=jsonpath='{.status.observedGeneration}'="${static_public_generation}" \
  "appdeployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=120s
kube wait --for=condition=Ready \
  "appdeployment/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=120s

kube apply -f "${SPA_MANIFEST}"
spa_generation="$(kube get "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.generation}')"
kube wait --for=jsonpath='{.status.observedGeneration}'="${spa_generation}" \
  "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=180s
kube wait --for=jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'=True \
  "httproute/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=120s
kube wait --for=jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}'=True \
  "httproute/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=120s
kube wait --for=condition=Ready \
  "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=180s
kube rollout status \
  "deployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=180s

wait_for_content() {
  local expected=$1
  local url=$2
  local response=""

  for _ in $(seq 1 60); do
    response="$(curl --connect-timeout 3 --max-time 5 --fail --silent "${url}")" || true
    if grep -Fq "${expected}" <<<"${response}"; then
      printf '%s' "${response}"
      return 0
    fi
    sleep 1
  done
  echo "expected ${url} to contain ${expected}" >&2
  return 1
}

wait_for_redirect() {
  local url=$1
  local status=""
  for _ in $(seq 1 30); do
    status="$(curl --connect-timeout 3 --max-time 5 \
      --silent --output /dev/null --write-out '%{http_code}' "${url}")" || true
    if [[ ${status} == "308" ]]; then
      return 0
    fi
    sleep 1
  done
  echo "expected ${url} to redirect with HTTP 308, got ${status:-no response}" >&2
  return 1
}

wait_for_redirect "http://${STATIC_HOSTNAME}/"
wait_for_redirect "http://${SPA_HOSTNAME}/"
static_response="$(wait_for_content 'data-profile="static-html"' "https://${STATIC_HOSTNAME}/")"
grep -Fq 'data-profile="static-html"' <<<"${static_response}"
wait_for_content 'name="fruto-version" content="v1"' \
  "https://${SPA_HOSTNAME}/projects/example" >/dev/null

curl --fail --silent --show-error --dump-header "${RESPONSE_HEADERS}" \
  --output "${RESPONSE_BODY}" "https://${STATIC_HOSTNAME}/"
tr -d '\r' <"${RESPONSE_HEADERS}" | grep -Fqi 'Cache-Control: no-cache'
curl --fail --silent --show-error --dump-header "${RESPONSE_HEADERS}" \
  --output /dev/null "https://${STATIC_HOSTNAME}/assets/styles-9a4b7c2d.css"
tr -d '\r' <"${RESPONSE_HEADERS}" |
  grep -Fqi 'Cache-Control: public, max-age=31536000, immutable'

missing_spa_asset_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
  "https://${SPA_HOSTNAME}/assets/missing.js")"
if [[ ${missing_spa_asset_status} != "404" ]]; then
  echo "expected the missing SPA asset to return 404, got ${missing_spa_asset_status}" >&2
  exit 1
fi

spa_app_uid_before="$(kube get "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"
spa_deployment_uid_before="$(kube get "deployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"
spa_service_uid_before="$(kube get "service/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"
spa_route_uid_before="$(kube get "httproute/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"

kube patch "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --type=merge \
  --patch "{\"spec\":{\"image\":\"${SPA_IMAGE_V2}\"}}"
spa_v2_generation="$(kube get "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.generation}')"
kube wait --for=jsonpath='{.status.observedGeneration}'="${spa_v2_generation}" \
  "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=120s
kube rollout status \
  "deployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=180s
kube wait --for=condition=Ready \
  "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" \
  --timeout=120s
wait_for_content 'name="fruto-version" content="v2"' \
  "https://${SPA_HOSTNAME}/" >/dev/null

spa_app_uid_after="$(kube get "appdeployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"
spa_deployment_uid_after="$(kube get "deployment/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"
spa_service_uid_after="$(kube get "service/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"
spa_route_uid_after="$(kube get "httproute/${SPA_APP}" \
  -n "${SPA_NAMESPACE}" -o jsonpath='{.metadata.uid}')"
if [[ ${spa_app_uid_after} != "${spa_app_uid_before}" ||
  ${spa_deployment_uid_after} != "${spa_deployment_uid_before}" ||
  ${spa_service_uid_after} != "${spa_service_uid_before}" ||
  ${spa_route_uid_after} != "${spa_route_uid_before}" ]]; then
  echo "k3s rollout replaced a logical Kubernetes child" >&2
  exit 1
fi

kube patch "service/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --type=merge \
  --patch '{"spec":{"selector":{"drift":"true"}}}'
kube wait \
  --for=jsonpath='{.spec.selector.platform\.fruto\.calouro\.tech/app-deployment}'="${STATIC_APP}" \
  "service/${STATIC_APP}" \
  -n "${STATIC_NAMESPACE}" \
  --timeout=120s
for _ in $(seq 1 60); do
  static_service_selector_drift="$(kube get "service/${STATIC_APP}" \
    -n "${STATIC_NAMESPACE}" -o jsonpath='{.spec.selector.drift}')"
  if [[ -z ${static_service_selector_drift} ]]; then
    break
  fi
  sleep 1
done
if [[ -n ${static_service_selector_drift} ]]; then
  echo "operator did not remove static frontend Service selector drift" >&2
  exit 1
fi

echo "frontend k3s validation passed"
echo "static frontend: https://${STATIC_HOSTNAME} (${STATIC_IMAGE})"
echo "SPA frontend: https://${SPA_HOSTNAME} (${SPA_IMAGE_V2})"
