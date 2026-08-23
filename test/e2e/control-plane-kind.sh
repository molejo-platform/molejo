#!/usr/bin/env bash
set -euo pipefail

if ! command -v kind >/dev/null 2>&1; then
  echo "kind is required for the control-plane E2E" >&2
  exit 2
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required for the control-plane E2E" >&2
  exit 2
fi

cluster_name="${FRUTO_KIND_CLUSTER:-fruto-control-plane}"
tmp_dir="$(mktemp -d)"
cleanup() {
  kind delete cluster --name "$cluster_name" >/dev/null 2>&1 || true
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

kind create cluster --name "$cluster_name" --kubeconfig "$tmp_dir/kubeconfig"
kubectl --kubeconfig "$tmp_dir/kubeconfig" apply -f deploy/crds/platform.fruto.calouro.tech_appdeployments.yaml
kubectl --kubeconfig "$tmp_dir/kubeconfig" apply -k deploy/operator
kubectl --kubeconfig "$tmp_dir/kubeconfig" wait --for=condition=Available deployment/platform-operator -n fruto-system --timeout=120s
echo "control-plane Kind bootstrap passed; API and PostgreSQL image injection remain environment-specific"
