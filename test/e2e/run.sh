#!/usr/bin/env bash

set -euo pipefail

readonly KIND_VERSION="v0.32.0"
readonly KIND_NODE_IMAGE="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"
readonly CLUSTER_NAME="fruto-e2e-$$"
readonly OPERATOR_IMAGE="fruto-platform-operator:e2e"
readonly KUBECONFIG_FILE="$(mktemp)"
readonly HEALTH_FORWARD_LOG="${KUBECONFIG_FILE}.health-port-forward.log"
readonly METRICS_FORWARD_LOG="${KUBECONFIG_FILE}.metrics-port-forward.log"

export OPERATOR_IMAGE

HEALTH_FORWARD_PID=""
METRICS_FORWARD_PID=""
HEALTH_LOCAL_PORT=""
METRICS_LOCAL_PORT=""

kind_cli() {
  go run "sigs.k8s.io/kind@${KIND_VERSION}" "$@"
}

start_port_forward() {
  local resource=$1
  local remote_port=$2
  local log_file=$3
  local pid_variable=$4
  local port_variable=$5

  kubectl --kubeconfig "${KUBECONFIG_FILE}" port-forward \
    -n fruto-system \
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

dump_diagnostics() {
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get pods -A -o wide || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get appdeployments -A -o yaml || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" describe \
    appdeployment/ap-e2e000001 -n ws-e2e || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" describe \
    deployment/ap-e2e000001 -n ws-e2e || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" describe \
    deployment/platform-operator -n fruto-system || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" logs \
    deployment/platform-operator -n fruto-system --all-containers --prefix || true
  kubectl --kubeconfig "${KUBECONFIG_FILE}" get events -A \
    --sort-by=.metadata.creationTimestamp || true
}

finish() {
  local exit_code=$?
  trap - EXIT

  if [[ -n ${HEALTH_FORWARD_PID} ]]; then
    kill "${HEALTH_FORWARD_PID}" >/dev/null 2>&1 || true
  fi
  if [[ -n ${METRICS_FORWARD_PID} ]]; then
    kill "${METRICS_FORWARD_PID}" >/dev/null 2>&1 || true
  fi

  if [[ ${exit_code} -ne 0 ]]; then
    dump_diagnostics
  fi

  kind_cli delete cluster --name "${CLUSTER_NAME}" >/dev/null 2>&1 || true
  rm -f "${KUBECONFIG_FILE}" "${HEALTH_FORWARD_LOG}" "${METRICS_FORWARD_LOG}"
  exit "${exit_code}"
}
trap finish EXIT

kind_cli create cluster \
  --name "${CLUSTER_NAME}" \
  --image "${KIND_NODE_IMAGE}" \
  --kubeconfig "${KUBECONFIG_FILE}" \
  --wait 180s

docker buildx build \
  --file services/platform-operator/Dockerfile \
  --tag "${OPERATOR_IMAGE}" \
  --load \
  .
bash test/container/run.sh
kind_cli load docker-image --name "${CLUSTER_NAME}" "${OPERATOR_IMAGE}"

kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -k deploy/crds
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Established \
  crd/appdeployments.platform.fruto.calouro.tech \
  --timeout=60s
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -k deploy/operator
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/platform-operator \
  -n fruto-system \
  --timeout=120s

start_port_forward deployment/platform-operator 8081 "${HEALTH_FORWARD_LOG}" \
  HEALTH_FORWARD_PID HEALTH_LOCAL_PORT
curl --fail --silent --show-error "http://127.0.0.1:${HEALTH_LOCAL_PORT}/healthz" >/dev/null
curl --fail --silent --show-error "http://127.0.0.1:${HEALTH_LOCAL_PORT}/readyz" >/dev/null

kubectl --kubeconfig "${KUBECONFIG_FILE}" create serviceaccount \
  metrics-reader-e2e -n fruto-system
kubectl --kubeconfig "${KUBECONFIG_FILE}" create clusterrolebinding \
  "platform-operator-metrics-reader-e2e-${CLUSTER_NAME}" \
  --clusterrole=platform-operator-metrics-reader \
  --serviceaccount=fruto-system:metrics-reader-e2e
start_port_forward service/platform-operator-metrics 8443 "${METRICS_FORWARD_LOG}" \
  METRICS_FORWARD_PID METRICS_LOCAL_PORT

unauthenticated_status="$(curl --insecure --silent --output /dev/null \
  --write-out '%{http_code}' "https://127.0.0.1:${METRICS_LOCAL_PORT}/metrics")"
if [[ ${unauthenticated_status} != "401" && ${unauthenticated_status} != "403" ]]; then
  echo "expected anonymous metrics request to be denied, got HTTP ${unauthenticated_status}" >&2
  exit 1
fi

kubectl --kubeconfig "${KUBECONFIG_FILE}" create namespace ws-e2e
kubectl --kubeconfig "${KUBECONFIG_FILE}" apply -f test/e2e/appdeployment.yaml
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=condition=Ready \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=180s
kubectl --kubeconfig "${KUBECONFIG_FILE}" rollout status \
  deployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=180s

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
  --patch '{"spec":{"replicas":2}}'
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=jsonpath='{.status.observedGeneration}'=2 \
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

kubectl --kubeconfig "${KUBECONFIG_FILE}" delete \
  appdeployment/ap-e2e000001 \
  -n ws-e2e \
  --wait=true
kubectl --kubeconfig "${KUBECONFIG_FILE}" wait \
  --for=delete \
  deployment/ap-e2e000001 \
  -n ws-e2e \
  --timeout=120s
