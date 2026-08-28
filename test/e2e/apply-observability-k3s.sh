#!/usr/bin/env bash
set -euo pipefail

command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 2; }
context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
[[ "$context" == "fruto-lab" ]] || { echo "observability lab deployment requires context fruto-lab" >&2; exit 2; }
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

kubectl --context "$context" -n molejo-observability get secret molejo-observability-credentials >/dev/null
kubectl --context "$context" -n fruto-control-plane get secret molejo-observability-reader >/dev/null
kubectl --context "$context" apply --server-side --dry-run=server -k deploy/observability-lab >/dev/null
kubectl --context "$context" apply --server-side -k deploy/observability-lab >/dev/null

kubectl --context "$context" -n molejo-observability rollout status statefulset/clickhouse --timeout=600s
kubectl --context "$context" -n molejo-observability rollout status statefulset/victoria-metrics --timeout=300s
kubectl --context "$context" -n molejo-observability rollout status deployment/otel-gateway --timeout=300s
kubectl --context "$context" -n molejo-observability rollout status deployment/otel-cluster --timeout=300s
kubectl --context "$context" -n molejo-observability rollout status daemonset/otel-agent --timeout=300s

printf 'observability stack applied to context %s\n' "$context"
