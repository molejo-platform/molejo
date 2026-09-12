#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <kind|installation|metrics-current|storage-rwo|publication-binding|registry-private> [--context <name>] [--storage-class <name>] [--gateway-file <path>] [--registry-file <path>]" >&2
  exit 2
}

[[ $# -ge 1 ]] || usage
profile="$1"
shift
context_name=""
storage_class=""
gateway_file=""
registry_file=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context)
      [[ $# -ge 2 ]] || usage
      context_name="$2"
      shift 2
      ;;
    --storage-class)
      [[ $# -ge 2 ]] || usage
      storage_class="$2"
      shift 2
      ;;
    --gateway-file)
      [[ $# -ge 2 ]] || usage
      gateway_file="$2"
      shift 2
      ;;
    --registry-file)
      [[ $# -ge 2 ]] || usage
      registry_file="$2"
      shift 2
      ;;
    *) usage ;;
  esac
done

repository_root="$(git rev-parse --show-toplevel)"

if [[ "$profile" == "kind" ]]; then
  [[ -z "$context_name" && -z "$storage_class" && -z "$gateway_file" && -z "$registry_file" ]] || usage
  exec "$repository_root/tools/testing/kind-conformance.sh"
fi

[[ -n "$context_name" ]] || usage
kubectl config get-contexts "$context_name" >/dev/null

molejoctl() {
  if [[ -n "${MOLEJOCTL_BIN:-}" ]]; then
    "$MOLEJOCTL_BIN" "$@"
    return
  fi
  (
    cd "$repository_root"
    go run ./apps/molejoctl "$@"
  )
}

case "$profile" in
  installation)
    "$repository_root/tools/testing/control-plane-k3s.sh" verify --context "$context_name"
    molejoctl platform doctor --kube-context "$context_name"
    ;;
  metrics-current)
    kubectl --context "$context_name" get --raw /apis/metrics.k8s.io/v1beta1/nodes >/dev/null
    echo "Kubernetes Metrics API is queryable in context $context_name"
    ;;
  storage-rwo)
    [[ -n "$storage_class" ]] || {
      echo "storage-rwo requires --storage-class" >&2
      exit 2
    }
    molejoctl capability storage smoke --kube-context "$context_name" --storage-class "$storage_class"
    ;;
  publication-binding)
    [[ -n "$gateway_file" ]] || {
      echo "publication-binding requires --gateway-file" >&2
      exit 2
    }
    molejoctl capability gateway verify --kube-context "$context_name" --file "$gateway_file"
    ;;
  registry-private)
    [[ -n "$registry_file" ]] || {
      echo "registry-private requires --registry-file" >&2
      exit 2
    }
    molejoctl capability registry verify --kube-context "$context_name" --file "$registry_file"
    molejoctl capability registry smoke --kube-context "$context_name" --file "$registry_file"
    ;;
  *) usage ;;
esac

echo "Conformance profile $profile passed in context $context_name"
