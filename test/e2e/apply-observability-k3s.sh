#!/usr/bin/env bash
set -euo pipefail

for command in kubectl grep; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done
context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
[[ "$context" == "fruto-lab" ]] || { echo "observability lab deployment requires context fruto-lab" >&2; exit 2; }
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR outside the checkout}"
release_file="${FRUTO_OBSERVABILITY_RELEASE_OUTPUT:?set FRUTO_OBSERVABILITY_RELEASE_OUTPUT to the rendered observability manifest}"
[[ -s "$release_file" ]] || { echo "observability release manifest does not exist" >&2; exit 2; }
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"
release_metadata_load "$release_dir/metadata/observability.env"
actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

kubectl --context "$context" -n molejo-observability get secret "$MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET" >/dev/null
grep -q "name: $MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET" "$release_file" || { echo "release does not reference prepared observability credentials" >&2; exit 1; }
kubectl --context "$context" -n molejo-observability delete job clickhouse-log-schema-migrate --ignore-not-found >/dev/null
kubectl --context "$context" apply --server-side --dry-run=server -f "$release_file" >/dev/null
kubectl --context "$context" apply --server-side -f "$release_file" >/dev/null

kubectl --context "$context" -n molejo-observability rollout status statefulset/clickhouse --timeout=600s
kubectl --context "$context" -n molejo-observability wait --for=condition=complete job/clickhouse-log-schema-migrate --timeout=300s
kubectl --context "$context" -n molejo-observability rollout status statefulset/victoria-metrics --timeout=300s
kubectl --context "$context" -n molejo-observability rollout status deployment/otel-gateway --timeout=300s
kubectl --context "$context" -n molejo-observability rollout status deployment/otel-cluster --timeout=300s
kubectl --context "$context" -n molejo-observability-agents rollout status daemonset/otel-agent --timeout=300s

printf 'observability stack applied to context %s\n' "$context"
