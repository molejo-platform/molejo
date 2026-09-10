#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <install|verify|teardown|cycle> --context <name> [--version <version>] [--confirm <name>]" >&2
  exit 2
}

[[ $# -ge 1 ]] || usage
mode="$1"
shift
context_name=""
version="${MOLEJO_VERSION:-}"
confirmation=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context)
      [[ $# -ge 2 ]] || usage
      context_name="$2"
      shift 2
      ;;
    --version)
      [[ $# -ge 2 ]] || usage
      version="$2"
      shift 2
      ;;
    --confirm)
      [[ $# -ge 2 ]] || usage
      confirmation="$2"
      shift 2
      ;;
    *) usage ;;
  esac
done

[[ -n "$context_name" ]] || usage
kubectl config get-contexts "$context_name" >/dev/null
repository_root="$(git rev-parse --show-toplevel)"

install_control_plane() {
  [[ -n "$version" ]] || {
    echo "--version or MOLEJO_VERSION is required for a development build" >&2
    exit 2
  }
  if [[ -n "${MOLEJOCTL_BIN:-}" ]]; then
    "$MOLEJOCTL_BIN" control-plane install --kube-context "$context_name" --version "$version"
    return
  fi
  (
    cd "$repository_root"
    go run ./apps/molejoctl platform control-plane install --kube-context "$context_name" --version "$version"
  )
}

agent_status() {
  local pod_name
  pod_name="$(kubectl --context "$context_name" -n molejo-system get pods -l app.kubernetes.io/name=cluster-agent -o jsonpath='{.items[0].metadata.name}')"
  kubectl --context "$context_name" get --raw "/api/v1/namespaces/molejo-system/pods/${pod_name}:8081/proxy/status"
}

check_equal() {
  local label="$1" actual="$2" expected="$3"
  if [[ "$actual" != "$expected" ]]; then
    echo "$label: got '$actual', want '$expected'" >&2
    return 1
  fi
}

verify_control_plane() {
  check_equal "PostgreSQL PVC phase" "$(kubectl --context "$context_name" -n molejo-control-plane get pvc data-postgres-0 -o jsonpath='{.status.phase}')" "Bound"
  check_equal "PostgreSQL ready replicas" "$(kubectl --context "$context_name" -n molejo-control-plane get statefulset postgres -o jsonpath='{.status.readyReplicas}')" "1"
  check_equal "bootstrap succeeded jobs" "$(kubectl --context "$context_name" -n molejo-control-plane get job control-plane-bootstrap -o jsonpath='{.status.succeeded}')" "1"
  check_equal "control-plane available replicas" "$(kubectl --context "$context_name" -n molejo-control-plane get deployment control-plane-api -o jsonpath='{.status.availableReplicas}')" "1"
  local status
  status="$(agent_status)"
  if ! grep -q '"state":"Paired"' <<<"$status"; then
    echo "cluster-agent is not paired: $status" >&2
    return 1
  fi
  if kubectl --context "$context_name" -n molejo-system get secret molejo-agent-enrollment -o jsonpath='{.data.token}' | grep -q .; then
    echo "Agent enrollment token was not cleared" >&2
    exit 1
  fi
  echo "control plane is healthy in context $context_name"
}

teardown_control_plane() {
  [[ "$confirmation" == "$context_name" ]] || {
    echo "teardown requires --confirm $context_name" >&2
    exit 2
  }
  kubectl --context "$context_name" delete namespace molejo-control-plane --ignore-not-found --wait=true --timeout=180s
  kubectl --context "$context_name" -n molejo-system delete configmap cluster-agent-connection cluster-agent-control-plane-ca --ignore-not-found
  kubectl --context "$context_name" -n molejo-system patch secret molejo-agent-identity --type=merge -p '{"data":null}' >/dev/null
  kubectl --context "$context_name" -n molejo-system patch secret molejo-agent-enrollment --type=merge -p '{"data":null}' >/dev/null
  kubectl --context "$context_name" -n molejo-system rollout restart deployment/cluster-agent >/dev/null
  kubectl --context "$context_name" -n molejo-system rollout status deployment/cluster-agent --timeout=120s >/dev/null
  if kubectl --context "$context_name" get namespace molejo-control-plane >/dev/null 2>&1; then
    echo "control plane namespace still exists" >&2
    exit 1
  fi
  echo "control plane and Agent identity removed from context $context_name"
}

case "$mode" in
  install) install_control_plane ;;
  verify) verify_control_plane ;;
  teardown) teardown_control_plane ;;
  cycle)
    [[ "$confirmation" == "$context_name" ]] || {
      echo "cycle requires --confirm $context_name" >&2
      exit 2
    }
    install_control_plane
    verify_control_plane
    install_control_plane
    verify_control_plane
    teardown_control_plane
    install_control_plane
    verify_control_plane
    ;;
  *) usage ;;
esac
