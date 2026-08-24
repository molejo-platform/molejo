#!/usr/bin/env bash

set -euo pipefail

readonly KIND_VERSION="v0.32.0"
readonly KIND_NODE_IMAGE="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"
readonly GATEWAY_API_URL="https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml"
readonly GATEWAY_API_SHA256="751002b3b91a87f7ae3bd2517c79a47a8d7ed6702901808a1cf9bd97d284f9b8"
readonly PERSISTENT_TRANSPORT_SECONDS=11
readonly CLUSTER_NAME="fruto-e2e-$$"
readonly OPERATOR_IMAGE="fruto-platform-operator:e2e-$$"
readonly FIXTURE_IMAGE_V1_TAG="fruto-phase2-http-app:e2e-v1-$$"
readonly FIXTURE_IMAGE_V2_TAG="fruto-phase2-http-app:e2e-v2-$$"
readonly FIXTURE_IMAGE_V1_FULL="docker.io/library/${FIXTURE_IMAGE_V1_TAG}"
readonly FIXTURE_IMAGE_V2_FULL="docker.io/library/${FIXTURE_IMAGE_V2_TAG}"
readonly STATIC_IMAGE_TAG="fruto-phase4-static-html:e2e-$$"
readonly SPA_IMAGE_V1_TAG="fruto-phase4-vite-react-spa:e2e-v1-$$"
readonly SPA_IMAGE_V2_TAG="fruto-phase4-vite-react-spa:e2e-v2-$$"
readonly STATIC_IMAGE_FULL="docker.io/library/${STATIC_IMAGE_TAG}"
readonly SPA_IMAGE_V1_FULL="docker.io/library/${SPA_IMAGE_V1_TAG}"
readonly SPA_IMAGE_V2_FULL="docker.io/library/${SPA_IMAGE_V2_TAG}"
readonly KUBECONFIG_FILE="$(mktemp)"
readonly APP_MANIFEST_FILE="$(mktemp)"
readonly STATIC_MANIFEST_FILE="$(mktemp)"
readonly SPA_MANIFEST_FILE="$(mktemp)"
readonly OPERATOR_MANIFEST_FILE="$(mktemp)"
readonly FIXTURE_V1_METADATA="$(mktemp)"
readonly FIXTURE_V2_METADATA="$(mktemp)"
readonly STATIC_METADATA="$(mktemp)"
readonly SPA_V1_METADATA="$(mktemp)"
readonly SPA_V2_METADATA="$(mktemp)"
readonly GATEWAY_API_MANIFEST_FILE="$(mktemp)"
readonly WILDCARD_CERT_FILE="$(mktemp)"
readonly WILDCARD_KEY_FILE="$(mktemp)"
readonly PUBLIC_SSE_OUTPUT="$(mktemp)"
readonly FRONTEND_BODY="$(mktemp)"
readonly FRONTEND_HEADERS="$(mktemp)"
readonly HEALTH_FORWARD_LOG="${KUBECONFIG_FILE}.health-port-forward.log"
readonly METRICS_FORWARD_LOG="${KUBECONFIG_FILE}.metrics-port-forward.log"
readonly APP_FORWARD_LOG="${KUBECONFIG_FILE}.app-port-forward.log"
readonly GATEWAY_FORWARD_LOG="${KUBECONFIG_FILE}.gateway-port-forward.log"
readonly OPERATOR_IDENTITY="system:serviceaccount:fruto-system:platform-operator"
readonly PUBLIC_EGRESS_URL="${E2E_PUBLIC_EGRESS_URL:-}"

export OPERATOR_IMAGE

HEALTH_FORWARD_PID=""
METRICS_FORWARD_PID=""
APP_FORWARD_PID=""
GATEWAY_FORWARD_PID=""
PUBLIC_SSE_PID=""
HEALTH_LOCAL_PORT=""
METRICS_LOCAL_PORT=""
APP_LOCAL_PORT=""
GATEWAY_LOCAL_PORT=""
FIXTURE_IMAGE_V1=""
FIXTURE_IMAGE_V2=""
STATIC_IMAGE=""
SPA_IMAGE_V1=""
SPA_IMAGE_V2=""

kind_cli() {
  go run "sigs.k8s.io/kind@${KIND_VERSION}" "$@"
}

assert_can_i() {
  local result
  result="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" auth can-i \
    --as="${OPERATOR_IDENTITY}" "$@")" || true
  if [[ ${result} != "yes" ]]; then
    echo "expected ${OPERATOR_IDENTITY} to be allowed: $*; got ${result:-no response}" >&2
    return 1
  fi
}

assert_cannot_i() {
  local result
  result="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" auth can-i \
    --as="${OPERATOR_IDENTITY}" "$@")" || true
  if [[ ${result} != "no" ]]; then
    echo "expected ${OPERATOR_IDENTITY} to be denied: $*; got ${result:-no response}" >&2
    return 1
  fi
}

wait_for_resource() {
  local resource=$1
  local namespace=$2
  local name=$3

  for _ in $(seq 1 60); do
    if kubectl --kubeconfig "${KUBECONFIG_FILE}" get "${resource}" \
      "${name}" -n "${namespace}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  echo "timed out waiting for ${resource} ${namespace}/${name}" >&2
  return 1
}

start_port_forward() {
  local namespace=$1
  local resource=$2
  local remote_port=$3
  local log_file=$4
  local pid_variable=$5
  local port_variable=$6

  kubectl --kubeconfig "${KUBECONFIG_FILE}" port-forward \
    -n "${namespace}" \
    "${resource}" \
    ":${remote_port}" >"${log_file}" 2>&1 &
  local forward_pid=$!
  local local_port=""

  for _ in $(seq 1 50); do
    local_port="$(sed -n 's/.*127\.0\.0\.1:\([0-9][0-9]*\).*/\1/p' "${log_file}" | head -n 1)"
    if [[ -n ${local_port} ]]; then
      printf -v "${pid_variable}" '%s' "${forward_pid}"
      printf -v "${port_variable}" '%s' "${local_port}"
      return 0
    fi
    if ! kill -0 "${forward_pid}" 2>/dev/null; then
      break
    fi
    sleep 0.2
  done

  cat "${log_file}" >&2
  return 1
}

stop_port_forward() {
  local pid=$1
  if [[ -n ${pid} ]]; then
    kill "${pid}" >/dev/null 2>&1 || true
  fi
}

containerd_manifest_digest() {
  local image=$1
  local digest
  digest="$(docker exec "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images inspect "${image}" | \
    sed -n 's/.*@\(sha256:[a-f0-9]\{64\}\).*/\1/p' | head -n 1)"
  if [[ ! ${digest} =~ ^sha256:[a-f0-9]{64}$ ]]; then
    echo "could not resolve the imported manifest digest for ${image}" >&2
    return 1
  fi
	printf '%s' "${digest}"
}

register_digest_reference() {
  local tagged_image=$1
  local digest=$2
  local repository="${tagged_image%:*}"
  docker exec "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images tag \
    "${tagged_image}" \
    "${repository}@${digest}" >/dev/null
}

curl_json() {
  local url=$1
  for _ in $(seq 1 30); do
    if curl --fail --silent --show-error "${url}"; then
      return 0
    fi
    sleep 1
  done
  echo "timed out requesting ${url}" >&2
  return 1
}

wait_for_public_status() {
  local expected_status=$1
  local hostname=$2
  local url=$3
  local status=""

  for _ in $(seq 1 60); do
    status="$(curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" \
      --connect-timeout 2 --max-time 3 --silent --output /dev/null \
      --write-out '%{http_code}' \
      --resolve "${hostname}:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
      "${url}")" || true
    if [[ ${status} == "${expected_status}" ]]; then
      return 0
    fi
    sleep 0.5
  done

  echo "expected ${url} to return HTTP ${expected_status}, got ${status:-no response}" >&2
  return 1
}

wait_for_public_content() {
  local expected=$1
  local hostname=$2
  local url=$3
  local response=""

  for _ in $(seq 1 60); do
    response="$(curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" \
      --connect-timeout 2 --max-time 3 --fail --silent \
      --resolve "${hostname}:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
      "${url}")" || true
    if grep -Fq "${expected}" <<<"${response}"; then
      printf '%s' "${response}"
      return 0
    fi
    sleep 0.5
  done

  echo "expected ${url} to contain ${expected}" >&2
  return 1
}

assert_response_header() {
  local headers_file=$1
  local expected=$2
  if ! tr -d '\r' <"${headers_file}" | grep -Fqi "${expected}"; then
    echo "expected response headers to contain ${expected}" >&2
    cat "${headers_file}" >&2
    return 1
  fi
}

dump_diagnostics() {
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get pods -A -o wide || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get appdeployments -A -o yaml || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get services -A -o wide || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get gateways,httproutes -A -o yaml || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" describe \
    appdeployment/ap-e2e000001 -n ws-e2e || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" describe \
    deployment/ap-e2e000001 -n ws-e2e || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" describe \
    service/ap-e2e000001 -n ws-e2e || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" describe \
    deployment/platform-operator -n fruto-system || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" logs \
    deployment/platform-operator -n fruto-system --all-containers --prefix || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" logs \
    deployment/traefik-e2e -n fruto-system --all-containers --prefix || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get events -A \
    --sort-by=.metadata.creationTimestamp || true
}

finish() {
  local exit_code=$?
  trap - EXIT

  stop_port_forward "${HEALTH_FORWARD_PID}"
  stop_port_forward "${METRICS_FORWARD_PID}"
  stop_port_forward "${APP_FORWARD_PID}"
  stop_port_forward "${GATEWAY_FORWARD_PID}"
  stop_port_forward "${PUBLIC_SSE_PID}"

  if [[ ${exit_code} -ne 0 ]]; then
    dump_diagnostics
  fi

  kind_cli delete cluster --name "${CLUSTER_NAME}" >/dev/null 2>&1 || true
  docker image rm \
    "${OPERATOR_IMAGE}" \
    "${FIXTURE_IMAGE_V1_TAG}" \
    "${FIXTURE_IMAGE_V2_TAG}" \
    "${STATIC_IMAGE_TAG}" \
    "${SPA_IMAGE_V1_TAG}" \
    "${SPA_IMAGE_V2_TAG}" >/dev/null 2>&1 || true
  rm -f \
    "${KUBECONFIG_FILE}" \
    "${APP_MANIFEST_FILE}" \
    "${STATIC_MANIFEST_FILE}" \
    "${SPA_MANIFEST_FILE}" \
    "${OPERATOR_MANIFEST_FILE}" \
    "${FIXTURE_V1_METADATA}" \
    "${FIXTURE_V2_METADATA}" \
    "${STATIC_METADATA}" \
    "${SPA_V1_METADATA}" \
    "${SPA_V2_METADATA}" \
    "${GATEWAY_API_MANIFEST_FILE}" \
    "${WILDCARD_CERT_FILE}" \
    "${WILDCARD_KEY_FILE}" \
    "${PUBLIC_SSE_OUTPUT}" \
    "${FRONTEND_BODY}" \
    "${FRONTEND_HEADERS}" \
    "${HEALTH_FORWARD_LOG}" \
    "${METRICS_FORWARD_LOG}" \
    "${APP_FORWARD_LOG}" \
    "${GATEWAY_FORWARD_LOG}"
  exit "${exit_code}"
}
trap finish EXIT

kind_cli create cluster \
  --name "${CLUSTER_NAME}" \
  --image "${KIND_NODE_IMAGE}" \
  --kubeconfig "${KUBECONFIG_FILE}" \
  --wait 180s

curl -L --fail --silent --show-error \
  "${GATEWAY_API_URL}" \
  --output "${GATEWAY_API_MANIFEST_FILE}"
echo "${GATEWAY_API_SHA256}  ${GATEWAY_API_MANIFEST_FILE}" | shasum -a 256 --check
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply --server-side \
  -f "${GATEWAY_API_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Established \
  crd/httproutes.gateway.networking.k8s.io \
  --timeout=60s
kubectl --kubeconfig "${KUBECONFIG_FILE}" create namespace fruto-system
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj "/CN=*.molejo.dev" \
  -addext "subjectAltName=DNS:*.molejo.dev" \
  -keyout "${WILDCARD_KEY_FILE}" \
  -out "${WILDCARD_CERT_FILE}" >/dev/null 2>&1
kubectl --kubeconfig "${KUBECONFIG_FILE}" create secret tls \
  fruto-e2e-wildcard-tls \
  -n fruto-system \
  --cert="${WILDCARD_CERT_FILE}" \
  --key="${WILDCARD_KEY_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -f test/e2e/gateway.yaml
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/traefik-e2e \
  -n fruto-system \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Programmed \
  gateway/fruto \
  -n fruto-system \
  --timeout=120s

docker buildx build \
  --file services/platform-operator/Dockerfile \
  --tag "${OPERATOR_IMAGE}" \
  --load \
  .
bash test/container/run.sh
kind_cli load docker-image --name "${CLUSTER_NAME}" "${OPERATOR_IMAGE}"

docker buildx build \
  --file test/fixtures/http-app/Dockerfile \
  --tag "${FIXTURE_IMAGE_V1_TAG}" \
  --build-arg VERSION=v1 \
  --metadata-file "${FIXTURE_V1_METADATA}" \
  --load \
  .
docker buildx build \
  --file test/fixtures/http-app/Dockerfile \
  --tag "${FIXTURE_IMAGE_V2_TAG}" \
  --build-arg VERSION=v2 \
  --metadata-file "${FIXTURE_V2_METADATA}" \
  --load \
  .
grep -q '"containerimage.digest"' "${FIXTURE_V1_METADATA}"
grep -q '"containerimage.digest"' "${FIXTURE_V2_METADATA}"
kind_cli load docker-image --name "${CLUSTER_NAME}" "${FIXTURE_IMAGE_V1_TAG}"
kind_cli load docker-image --name "${CLUSTER_NAME}" "${FIXTURE_IMAGE_V2_TAG}"
fixture_v1_digest="$(containerd_manifest_digest "${FIXTURE_IMAGE_V1_FULL}")"
fixture_v2_digest="$(containerd_manifest_digest "${FIXTURE_IMAGE_V2_FULL}")"
register_digest_reference "${FIXTURE_IMAGE_V1_FULL}" "${fixture_v1_digest}"
register_digest_reference "${FIXTURE_IMAGE_V2_FULL}" "${fixture_v2_digest}"
FIXTURE_IMAGE_V1="${FIXTURE_IMAGE_V1_FULL%:*}@${fixture_v1_digest}"
FIXTURE_IMAGE_V2="${FIXTURE_IMAGE_V2_FULL%:*}@${fixture_v2_digest}"

docker buildx build \
  --file test/fixtures/static-html/Dockerfile \
  --tag "${STATIC_IMAGE_TAG}" \
  --metadata-file "${STATIC_METADATA}" \
  --load \
  .
docker buildx build \
  --file test/fixtures/vite-react-spa/Dockerfile \
  --tag "${SPA_IMAGE_V1_TAG}" \
  --build-arg APP_VERSION=v1 \
  --metadata-file "${SPA_V1_METADATA}" \
  --load \
  .
docker buildx build \
  --file test/fixtures/vite-react-spa/Dockerfile \
  --tag "${SPA_IMAGE_V2_TAG}" \
  --build-arg APP_VERSION=v2 \
  --metadata-file "${SPA_V2_METADATA}" \
  --load \
  .
for metadata_file in "${STATIC_METADATA}" "${SPA_V1_METADATA}" "${SPA_V2_METADATA}"; do
  grep -q '"containerimage.digest"' "${metadata_file}"
done
kind_cli load docker-image --name "${CLUSTER_NAME}" "${STATIC_IMAGE_TAG}"
kind_cli load docker-image --name "${CLUSTER_NAME}" "${SPA_IMAGE_V1_TAG}"
kind_cli load docker-image --name "${CLUSTER_NAME}" "${SPA_IMAGE_V2_TAG}"
static_digest="$(containerd_manifest_digest "${STATIC_IMAGE_FULL}")"
spa_v1_digest="$(containerd_manifest_digest "${SPA_IMAGE_V1_FULL}")"
spa_v2_digest="$(containerd_manifest_digest "${SPA_IMAGE_V2_FULL}")"
register_digest_reference "${STATIC_IMAGE_FULL}" "${static_digest}"
register_digest_reference "${SPA_IMAGE_V1_FULL}" "${spa_v1_digest}"
register_digest_reference "${SPA_IMAGE_V2_FULL}" "${spa_v2_digest}"
STATIC_IMAGE="${STATIC_IMAGE_FULL%:*}@${static_digest}"
SPA_IMAGE_V1="${SPA_IMAGE_V1_FULL%:*}@${spa_v1_digest}"
SPA_IMAGE_V2="${SPA_IMAGE_V2_FULL%:*}@${spa_v2_digest}"

kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -k deploy/crds
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Established \
  crd/appdeployments.platform.fruto.calouro.tech \
  --timeout=60s
kubectl kustomize deploy/operator |
  sed "s|image: ghcr.io/fruto-platform/platform-operator@sha256:0000000000000000000000000000000000000000000000000000000000000000|image: ${OPERATOR_IMAGE}|" >"${OPERATOR_MANIFEST_FILE}"
grep -Fq "image: ${OPERATOR_IMAGE}" "${OPERATOR_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -f "${OPERATOR_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/platform-operator \
  -n fruto-system \
  --timeout=120s

for verb in get list watch; do
  assert_can_i "${verb}" appdeployments.platform.fruto.calouro.tech -n fruto-system
done
for verb in get patch update; do
  assert_can_i "${verb}" appdeployments.platform.fruto.calouro.tech \
    --subresource=status -n fruto-system
done
for resource in deployments.apps services; do
  for verb in create get list patch update watch; do
    assert_can_i "${verb}" "${resource}" -n fruto-system
  done
  assert_cannot_i delete "${resource}" -n fruto-system
done
for verb in create get list patch update watch delete; do
  assert_can_i "${verb}" httproutes.gateway.networking.k8s.io -n fruto-system
done
for verb in create patch; do
  assert_can_i "${verb}" events -n fruto-system
done
assert_can_i create tokenreviews.authentication.k8s.io -A
assert_can_i create subjectaccessreviews.authorization.k8s.io -A

for verb in create patch update delete; do
  assert_cannot_i "${verb}" appdeployments.platform.fruto.calouro.tech -n fruto-system
done
assert_cannot_i get secrets -n fruto-system
assert_cannot_i get pods --subresource=log -n fruto-system
assert_cannot_i create pods --subresource=exec -n fruto-system
assert_cannot_i get nodes -A
assert_cannot_i create clusterroles.rbac.authorization.k8s.io -A
assert_cannot_i impersonate users -A
assert_cannot_i update deployments.apps --subresource=status -n fruto-system

start_port_forward fruto-system deployment/platform-operator 8081 "${HEALTH_FORWARD_LOG}" \
  HEALTH_FORWARD_PID HEALTH_LOCAL_PORT
curl --fail --silent --show-error "http://127.0.0.1:${HEALTH_LOCAL_PORT}/healthz" >/dev/null
curl --fail --silent --show-error "http://127.0.0.1:${HEALTH_LOCAL_PORT}/readyz" >/dev/null

kubectl --kubeconfig "${KUBECONFIG_FILE}" create serviceaccount \
  metrics-reader-e2e -n fruto-system
kubectl --kubeconfig "${KUBECONFIG_FILE}" create clusterrolebinding \
  "platform-operator-metrics-reader-e2e-${CLUSTER_NAME}" \
  --clusterrole=platform-operator-metrics-reader \
  --serviceaccount=fruto-system:metrics-reader-e2e
start_port_forward fruto-system service/platform-operator-metrics 8443 "${METRICS_FORWARD_LOG}" \
  METRICS_FORWARD_PID METRICS_LOCAL_PORT

unauthenticated_status="$(curl --insecure --silent --output /dev/null \
  --write-out '%{http_code}' "https://127.0.0.1:${METRICS_LOCAL_PORT}/metrics")"
if [[ ${unauthenticated_status} != "401" && ${unauthenticated_status} != "403" ]]; then
  echo "expected anonymous metrics request to be denied, got HTTP ${unauthenticated_status}" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" create namespace external-e2e
kubectl --kubeconfig "${KUBECONFIG_FILE}" create deployment phase2-upstream \
  -n external-e2e \
  --image="${FIXTURE_IMAGE_V1}" \
  --port=8080
kubectl --kubeconfig "${KUBECONFIG_FILE}" expose deployment phase2-upstream \
  -n external-e2e \
  --port=8080 \
  --target-port=8080
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/phase2-upstream \
  -n external-e2e \
  --timeout=120s

kubectl --kubeconfig "${KUBECONFIG_FILE}" create namespace ws-e2e
sed "s|__PHASE2_APP_IMAGE__|${FIXTURE_IMAGE_V1}|" \
  test/e2e/appdeployment.yaml >"${APP_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -f "${APP_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=180s

app_uid="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.uid}')"
service_type="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.type}')"
service_cluster_ip="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.clusterIP}')"
service_owner_uid="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.ownerReferences[0].uid}')"
service_port="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.ports[0].port}')"
service_target_port="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.ports[0].targetPort}')"
if [[ ${service_type} != "ClusterIP" || -z ${service_cluster_ip} ||
  ${service_owner_uid} != "${app_uid}" || ${service_port} != "8080" ||
  ${service_target_port} != "http" ]]; then
  echo "managed Service is not private or does not match the AppDeployment" >&2
  exit 1
fi
if kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  ingress.networking.k8s.io/ap-e2e000001 -n ws-e2e >/dev/null 2>&1; then
  echo "unexpected public Ingress for the private AppDeployment" >&2
  exit 1
fi
if kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  httproute.gateway.networking.k8s.io/ap-e2e000001 -n ws-e2e >/dev/null 2>&1; then
  echo "unexpected public HTTPRoute for the private AppDeployment" >&2
  exit 1
fi

run_as_non_root="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.spec.template.spec.securityContext.runAsNonRoot}')"
seccomp_type="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.spec.template.spec.securityContext.seccompProfile.type}')"
allow_escalation="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation}')"
read_only_root="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem}')"
dropped_capability="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.spec.template.spec.containers[0].securityContext.capabilities.drop[0]}')"
if [[ ${run_as_non_root} != "true" || ${seccomp_type} != "RuntimeDefault" ||
  ${allow_escalation} != "false" || ${read_only_root} != "true" ||
  ${dropped_capability} != "ALL" ]]; then
  echo "managed Deployment does not enforce the restricted runtime contract" >&2
  exit 1
fi

start_port_forward ws-e2e service/ap-e2e000001 8080 "${APP_FORWARD_LOG}" \
  APP_FORWARD_PID APP_LOCAL_PORT
root_response="$(curl_json "http://127.0.0.1:${APP_LOCAL_PORT}/")"
grep -q '"status":"ok"' <<<"${root_response}"
grep -q '"version":"v1"' <<<"${root_response}"
upstream_response="$(curl --fail --silent --show-error --get \
  --data-urlencode 'url=http://phase2-upstream.external-e2e.svc.cluster.local:8080/' \
  "http://127.0.0.1:${APP_LOCAL_PORT}/outbound")"
grep -q '"upstreamStatus":200' <<<"${upstream_response}"
grep -q '\\"version\\":\\"v1\\"' <<<"${upstream_response}"
if [[ -n ${PUBLIC_EGRESS_URL} ]]; then
  public_response="$(curl --fail --silent --show-error --get \
    --data-urlencode "url=${PUBLIC_EGRESS_URL}" \
    "http://127.0.0.1:${APP_LOCAL_PORT}/outbound")"
  grep -q '"upstreamStatus":200' <<<"${public_response}"
fi

start_port_forward fruto-system service/traefik-e2e 8443 "${GATEWAY_FORWARD_LOG}" \
  GATEWAY_FORWARD_PID GATEWAY_LOCAL_PORT
wait_for_public_status 404 phase3-e2e.molejo.dev \
  "https://phase3-e2e.molejo.dev:${GATEWAY_LOCAL_PORT}/"

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --type=merge \
  --patch '{"spec":{"exposure":"Public","slug":"phase3-e2e"}}'
wait_for_resource httproute.gateway.networking.k8s.io ws-e2e ap-e2e000001
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'=True \
  httproute/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}'=True \
  httproute/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s

public_base_url="https://phase3-e2e.molejo.dev:${GATEWAY_LOCAL_PORT}"
public_rest="$(curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" --fail --silent --show-error \
  --resolve "phase3-e2e.molejo.dev:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
  "${public_base_url}/")"
grep -q '"status":"ok"' <<<"${public_rest}"
public_graphql="$(curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" --fail --silent --show-error \
  --resolve "phase3-e2e.molejo.dev:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
  --header 'Content-Type: application/json' \
  --data '{"query":"{ status version }"}' \
  "${public_base_url}/graphql")"
grep -q '"data":{"status":"ok"' <<<"${public_graphql}"
curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" --fail --silent --show-error --no-buffer \
  --resolve "phase3-e2e.molejo.dev:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
  "${public_base_url}/events" >"${PUBLIC_SSE_OUTPUT}" &
PUBLIC_SSE_PID=$!
for _ in $(seq 1 50); do
  if grep -q '^event: status$' "${PUBLIC_SSE_OUTPUT}"; then
    break
  fi
  if ! kill -0 "${PUBLIC_SSE_PID}" 2>/dev/null; then
    echo "public SSE stream ended before its first event" >&2
    exit 1
  fi
  sleep 0.1
done
grep -q '^event: status$' "${PUBLIC_SSE_OUTPUT}"
initial_sse_event_count="$(grep -c '^event: status$' "${PUBLIC_SSE_OUTPUT}")"
for _ in $(seq 1 $((PERSISTENT_TRANSPORT_SECONDS * 5))); do
  if ! kill -0 "${PUBLIC_SSE_PID}" 2>/dev/null; then
    echo "public SSE stream ended before ${PERSISTENT_TRANSPORT_SECONDS}s" >&2
    exit 1
  fi
  sleep 0.2
done
final_sse_event_count="$(grep -c '^event: status$' "${PUBLIC_SSE_OUTPUT}")"
if ((final_sse_event_count <= initial_sse_event_count)); then
  echo "public SSE stream did not deliver incremental events" >&2
  exit 1
fi
stop_port_forward "${PUBLIC_SSE_PID}"
wait "${PUBLIC_SSE_PID}" 2>/dev/null || true
PUBLIC_SSE_PID=""
GOCACHE=/tmp/fruto-go-cache go run ./test/fixtures/transport-client \
  --address "127.0.0.1:${GATEWAY_LOCAL_PORT}" \
  --ca "${WILDCARD_CERT_FILE}" \
  --idle-duration "${PERSISTENT_TRANSPORT_SECONDS}s" \
  --url "wss://phase3-e2e.molejo.dev:${GATEWAY_LOCAL_PORT}/ws"

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --type=json \
  --patch '[{"op":"replace","path":"/spec/exposure","value":"Private"},{"op":"remove","path":"/spec/slug"}]'
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=delete \
  httproute/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
wait_for_public_status 404 phase3-e2e.molejo.dev "${public_base_url}/"
curl_json "http://127.0.0.1:${APP_LOCAL_PORT}/" >/dev/null

metrics_token="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" create token \
  metrics-reader-e2e -n fruto-system --duration=10m)"
metrics_output="$(curl --insecure --fail --silent --show-error \
  --header "Authorization: Bearer ${metrics_token}" \
  "https://127.0.0.1:${METRICS_LOCAL_PORT}/metrics")"
grep -q '^fruto_platform_operator_build_info' <<<"${metrics_output}"
grep -q '^fruto_platform_operator_state_transitions_total' <<<"${metrics_output}"
grep -q '^controller_runtime_reconcile_total' <<<"${metrics_output}"

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --type=merge \
  --patch '{"spec":{"probes":{"readiness":{"path":"/not-ready"}}}}'
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready=False \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Progressing \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" patch \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --type=merge \
  --patch '{"spec":{"probes":{"readiness":{"path":"/readyz"}}}}'
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --type=merge \
  --patch "{\"spec\":{\"image\":\"${FIXTURE_IMAGE_V2}\",\"replicas\":2}}"
updated_generation="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.generation}')"
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.observedGeneration}'="${updated_generation}" \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s

stop_port_forward "${APP_FORWARD_PID}"
APP_FORWARD_PID=""
start_port_forward ws-e2e service/ap-e2e000001 8080 "${APP_FORWARD_LOG}" \
  APP_FORWARD_PID APP_LOCAL_PORT
updated_response="$(curl_json "http://127.0.0.1:${APP_LOCAL_PORT}/")"
grep -q '"version":"v2"' <<<"${updated_response}"

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch service/ap-e2e000001 \
  -n ws-e2e \
  --type=merge \
  --patch '{"spec":{"type":"LoadBalancer","externalIPs":["192.0.2.10"],"loadBalancerSourceRanges":["192.0.2.0/24"],"selector":{"drift":"true"}}}'
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.spec.type}'=ClusterIP \
  service/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
service_external_ips="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.externalIPs}')"
service_load_balancer_source_ranges="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.loadBalancerSourceRanges}')"
service_selector="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.spec.selector.platform\.fruto\.calouro\.tech/app-deployment}')"
if [[ -n ${service_external_ips} || -n ${service_load_balancer_source_ranges} ||
  ${service_selector} != "ap-e2e000001" ]]; then
  echo "operator did not correct private Service drift" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch deployment/ap-e2e000001 \
  -n ws-e2e \
  --type=merge \
  --patch '{"spec":{"paused":true,"strategy":{"type":"Recreate","rollingUpdate":null},"minReadySeconds":60,"progressDeadlineSeconds":1200,"revisionHistoryLimit":1}}'
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.spec.strategy.type}'=RollingUpdate \
  deployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
deployment_paused="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.paused}')"
deployment_min_ready="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.minReadySeconds}')"
deployment_progress_deadline="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.progressDeadlineSeconds}')"
deployment_revision_history="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.revisionHistoryLimit}')"
if [[ ${deployment_paused} == "true" ||
  ( -n ${deployment_min_ready} && ${deployment_min_ready} != "0" ) ||
  ${deployment_progress_deadline} != "600" || ${deployment_revision_history} != "10" ]]; then
  echo "operator did not correct Deployment rollout drift" >&2
  exit 1
fi

app_uid_before_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.uid}')"
app_generation_before_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.generation}')"
deployment_uid_before_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.uid}')"
service_uid_before_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.uid}')"
expected_image="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.image}')"
ready_transition_before_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].lastTransitionTime}')"

kubectl --kubeconfig "${KUBECONFIG_FILE}" scale \
  deployment/platform-operator -n fruto-system --replicas=0
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=delete pod \
  -l app.kubernetes.io/name=platform-operator \
  -n fruto-system \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" delete \
  deployment/ap-e2e000001 service/ap-e2e000001 -n ws-e2e --wait=true

app_generation_while_stopped="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.generation}')"
if [[ ${app_generation_while_stopped} != "${app_generation_before_restart}" ]]; then
  echo "AppDeployment generation changed while the operator was stopped" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" scale \
  deployment/platform-operator -n fruto-system --replicas=1
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/platform-operator -n fruto-system --timeout=120s
wait_for_resource deployment ws-e2e ap-e2e000001
wait_for_resource service ws-e2e ap-e2e000001
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-e2e000001 -n ws-e2e --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s

app_uid_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.uid}')"
app_generation_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.generation}')"
deployment_uid_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.uid}')"
service_uid_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.uid}')"
deployment_owner_uid="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.ownerReferences[0].uid}')"
service_owner_uid="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-e2e000001 -n ws-e2e -o jsonpath='{.metadata.ownerReferences[0].uid}')"
deployment_image="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.template.spec.containers[0].image}')"
deployment_replicas="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.spec.replicas}')"
app_observed_generation_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e -o jsonpath='{.status.observedGeneration}')"
ready_status_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')"
ready_reason_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}')"
ready_transition_after_restart="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-e2e000001 -n ws-e2e \
  -o jsonpath='{.status.conditions[?(@.type=="Ready")].lastTransitionTime}')"

if [[ ${app_uid_after_restart} != "${app_uid_before_restart}" ||
  ${app_generation_after_restart} != "${app_generation_before_restart}" ]]; then
  echo "AppDeployment identity or generation changed during operator recovery" >&2
  exit 1
fi
if [[ ${deployment_uid_after_restart} == "${deployment_uid_before_restart}" ||
  ${service_uid_after_restart} == "${service_uid_before_restart}" ]]; then
  echo "expected both deleted children to be recreated with new UIDs" >&2
  exit 1
fi
if [[ ${deployment_owner_uid} != "${app_uid_before_restart}" ||
  ${service_owner_uid} != "${app_uid_before_restart}" ]]; then
  echo "recreated children do not reference the AppDeployment as controller owner" >&2
  exit 1
fi
if [[ ${deployment_image} != "${expected_image}" || ${deployment_replicas} != "2" ]]; then
  echo "recreated Deployment did not converge to the declared image and replicas" >&2
  exit 1
fi
if [[ ${app_observed_generation_after_restart} != "${app_generation_before_restart}" ||
  ${ready_status_after_restart} != "True" ||
  ${ready_reason_after_restart} != "DeploymentAvailable" ]]; then
  echo "AppDeployment status did not converge after operator recovery" >&2
  exit 1
fi
if [[ ${ready_transition_after_restart} == "${ready_transition_before_restart}" ]]; then
  echo "AppDeployment retained a stale Ready transition after operator recovery" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" delete \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --wait=true
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=delete \
  deployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=delete \
  service/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s

stop_port_forward "${APP_FORWARD_PID}"
APP_FORWARD_PID=""

kubectl --kubeconfig "${KUBECONFIG_FILE}" create namespace ws-static-e2e
sed "s|__STATIC_IMAGE__|${STATIC_IMAGE}|" \
  test/e2e/static-appdeployment.yaml >"${STATIC_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -f "${STATIC_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-static000001 \
  -n ws-static-e2e \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-static000001 \
  -n ws-static-e2e \
  --timeout=180s
if kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  httproute/ap-static000001 -n ws-static-e2e >/dev/null 2>&1; then
  echo "unexpected HTTPRoute for the private static frontend" >&2
  exit 1
fi

start_port_forward ws-static-e2e service/ap-static000001 8080 "${APP_FORWARD_LOG}" \
  APP_FORWARD_PID APP_LOCAL_PORT
curl --fail --silent --show-error --dump-header "${FRONTEND_HEADERS}" \
  --output "${FRONTEND_BODY}" "http://127.0.0.1:${APP_LOCAL_PORT}/"
grep -Fq 'data-profile="static-html"' "${FRONTEND_BODY}"
assert_response_header "${FRONTEND_HEADERS}" 'Cache-Control: no-cache'
curl --fail --silent --show-error --dump-header "${FRONTEND_HEADERS}" \
  --output /dev/null \
  "http://127.0.0.1:${APP_LOCAL_PORT}/assets/styles-9a4b7c2d.css"
assert_response_header "${FRONTEND_HEADERS}" \
  'Cache-Control: public, max-age=31536000, immutable'
stop_port_forward "${APP_FORWARD_PID}"
APP_FORWARD_PID=""

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch \
  appdeployment/ap-static000001 \
  -n ws-static-e2e \
  --type=merge \
  --patch '{"spec":{"exposure":"Public","slug":"phase4-static"}}'
wait_for_resource httproute.gateway.networking.k8s.io ws-static-e2e ap-static000001
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'=True \
  httproute/ap-static000001 \
  -n ws-static-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}'=True \
  httproute/ap-static000001 \
  -n ws-static-e2e \
  --timeout=120s
static_public_generation="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-static000001 -n ws-static-e2e -o jsonpath='{.metadata.generation}')"
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.observedGeneration}'="${static_public_generation}" \
  appdeployment/ap-static000001 \
  -n ws-static-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-static000001 \
  -n ws-static-e2e \
  --timeout=120s

static_public_url="https://phase4-static.molejo.dev:${GATEWAY_LOCAL_PORT}"
wait_for_public_status 200 phase4-static.molejo.dev \
  "${static_public_url}/"
curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" --fail --silent --show-error \
  --resolve "phase4-static.molejo.dev:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
  --dump-header "${FRONTEND_HEADERS}" \
  --output "${FRONTEND_BODY}" \
  "${static_public_url}/"
grep -Fq 'data-profile="static-html"' "${FRONTEND_BODY}"
assert_response_header "${FRONTEND_HEADERS}" 'Cache-Control: no-cache'
wait_for_public_status 404 phase4-static.molejo.dev \
  "${static_public_url}/missing"
wait_for_public_status 404 phase4-static.molejo.dev \
  "${static_public_url}/assets/missing.css"

static_app_uid="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-static000001 -n ws-static-e2e -o jsonpath='{.metadata.uid}')"
static_service_owner_uid="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-static000001 -n ws-static-e2e -o jsonpath='{.metadata.ownerReferences[0].uid}')"
static_route_owner_uid="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  httproute/ap-static000001 -n ws-static-e2e -o jsonpath='{.metadata.ownerReferences[0].uid}')"
if [[ ${static_service_owner_uid} != "${static_app_uid}" ||
  ${static_route_owner_uid} != "${static_app_uid}" ]]; then
  echo "static frontend children are not owned by the AppDeployment" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" patch service/ap-static000001 \
  -n ws-static-e2e \
  --type=merge \
  --patch '{"spec":{"selector":{"drift":"true"}}}'
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.spec.selector.platform\.fruto\.calouro\.tech/app-deployment}'=ap-static000001 \
  service/ap-static000001 \
  -n ws-static-e2e \
  --timeout=120s
for _ in $(seq 1 60); do
  static_service_selector_drift="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
    service/ap-static000001 -n ws-static-e2e -o jsonpath='{.spec.selector.drift}')"
  if [[ -z ${static_service_selector_drift} ]]; then
    break
  fi
  sleep 1
done
if [[ -n ${static_service_selector_drift} ]]; then
  echo "operator did not remove static frontend Service selector drift" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" create namespace ws-spa-e2e
sed "s|__SPA_IMAGE__|${SPA_IMAGE_V1}|" \
  test/e2e/spa-appdeployment.yaml >"${SPA_MANIFEST_FILE}"
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -f "${SPA_MANIFEST_FILE}"
wait_for_resource httproute.gateway.networking.k8s.io ws-spa-e2e ap-spa000001
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'=True \
  httproute/ap-spa000001 \
  -n ws-spa-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}'=True \
  httproute/ap-spa000001 \
  -n ws-spa-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-spa000001 \
  -n ws-spa-e2e \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-spa000001 \
  -n ws-spa-e2e \
  --timeout=180s

spa_public_url="https://phase4-spa.molejo.dev:${GATEWAY_LOCAL_PORT}"
wait_for_public_status 200 phase4-spa.molejo.dev \
  "${spa_public_url}/projects/example"
curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" --fail --silent --show-error \
  --resolve "phase4-spa.molejo.dev:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
  --dump-header "${FRONTEND_HEADERS}" \
  --output "${FRONTEND_BODY}" \
  "${spa_public_url}/projects/example"
grep -Fq 'name="fruto-profile" content="vite-react-spa"' "${FRONTEND_BODY}"
grep -Fq 'name="fruto-version" content="v1"' "${FRONTEND_BODY}"
assert_response_header "${FRONTEND_HEADERS}" 'Cache-Control: no-cache'
spa_asset="$(sed -n 's|.*src="\(/assets/[^\"]*\.js\)".*|\1|p' "${FRONTEND_BODY}" | head -n 1)"
if [[ -z ${spa_asset} ]]; then
  echo "could not discover the fingerprinted SPA asset through the public route" >&2
  exit 1
fi
curl --noproxy '*' --cacert "${WILDCARD_CERT_FILE}" --fail --silent --show-error \
  --resolve "phase4-spa.molejo.dev:${GATEWAY_LOCAL_PORT}:127.0.0.1" \
  --dump-header "${FRONTEND_HEADERS}" \
  --output /dev/null \
  "${spa_public_url}${spa_asset}"
assert_response_header "${FRONTEND_HEADERS}" \
  'Cache-Control: public, max-age=31536000, immutable'
wait_for_public_status 404 phase4-spa.molejo.dev \
  "${spa_public_url}/assets/missing.js"
wait_for_public_status 404 phase4-spa.molejo.dev \
  "${spa_public_url}/missing.css"

spa_app_uid_before="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
spa_deployment_uid_before="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
spa_service_uid_before="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
spa_route_uid_before="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  httproute/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
kubectl --kubeconfig "${KUBECONFIG_FILE}" patch \
  appdeployment/ap-spa000001 \
  -n ws-spa-e2e \
  --type=merge \
  --patch "{\"spec\":{\"image\":\"${SPA_IMAGE_V2}\"}}"
spa_updated_generation="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.generation}')"
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.observedGeneration}'="${spa_updated_generation}" \
  appdeployment/ap-spa000001 \
  -n ws-spa-e2e \
  --timeout=120s
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-spa000001 \
  -n ws-spa-e2e \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-spa000001 \
  -n ws-spa-e2e \
  --timeout=120s
spa_v2_response="$(wait_for_public_content \
  'name="fruto-version" content="v2"' \
  phase4-spa.molejo.dev \
  "${spa_public_url}/")"
grep -Fq 'name="fruto-version" content="v2"' <<<"${spa_v2_response}"

spa_app_uid_after="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  appdeployment/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
spa_deployment_uid_after="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  deployment/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
spa_service_uid_after="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  service/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
spa_route_uid_after="$(kubectl --kubeconfig "${KUBECONFIG_FILE}" get \
  httproute/ap-spa000001 -n ws-spa-e2e -o jsonpath='{.metadata.uid}')"
if [[ ${spa_app_uid_after} != "${spa_app_uid_before}" ||
  ${spa_deployment_uid_after} != "${spa_deployment_uid_before}" ||
  ${spa_service_uid_after} != "${spa_service_uid_before}" ||
  ${spa_route_uid_after} != "${spa_route_uid_before}" ]]; then
  echo "SPA rollout replaced a logical Kubernetes child" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" delete \
  appdeployment/ap-static000001 -n ws-static-e2e --wait=true
kubectl --kubeconfig "${KUBECONFIG_FILE}" delete \
  appdeployment/ap-spa000001 -n ws-spa-e2e --wait=true
for child in deployment service httproute.gateway.networking.k8s.io; do
  kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
    --for=delete "${child}/ap-static000001" \
    -n ws-static-e2e \
    --timeout=120s
  kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
    --for=delete "${child}/ap-spa000001" \
    -n ws-spa-e2e \
    --timeout=120s
done
