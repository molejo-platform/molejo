#!/usr/bin/env bash
set -euo pipefail

for command in kubectl grep; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done
context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
[[ "$context" == "fruto-lab" ]] || { echo "observability acceptance requires context fruto-lab" >&2; exit 2; }
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

for target in statefulset/clickhouse statefulset/victoria-metrics deployment/otel-gateway deployment/otel-cluster; do
  kubectl --context "$context" -n molejo-observability rollout status "$target" --timeout=60s >/dev/null
done
kubectl --context "$context" -n molejo-observability-agents rollout status daemonset/otel-agent --timeout=60s >/dev/null
kubectl --context "$context" -n molejo-observability wait --for=condition=complete job/clickhouse-log-schema-migrate --timeout=60s >/dev/null

for component in otel-gateway otel-cluster; do
  if kubectl --context "$context" -n molejo-observability logs -l app.kubernetes.io/name="$component" --tail=200 --prefix 2>&1 | grep -Eiq 'failed to start|error decoding|invalid configuration|permanent error'; then
    echo "$component reported a configuration or permanent export error" >&2
    exit 1
  fi
done
if kubectl --context "$context" -n molejo-observability-agents logs -l app.kubernetes.io/name=otel-agent --tail=200 --prefix 2>&1 | grep -Eiq 'failed to start|error decoding|invalid configuration|permanent error'; then
  echo "otel-agent reported a configuration or permanent export error" >&2
  exit 1
fi

printf 'observability workloads and collector configurations accepted on %s\n' "$context"
