#!/usr/bin/env bash
set -euo pipefail

for command in kubectl grep curl openssl base64 jq sort cut awk mktemp install; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done
context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
[[ "$context" == "fruto-lab" ]] || { echo "observability acceptance requires context fruto-lab" >&2; exit 2; }
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR outside the checkout}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"
release_metadata_load "$release_dir/metadata/observability.env"
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

temporary="$(mktemp -d)"
gateway_port_forward_pid=""
clickhouse_port_forward_pid=""
cleanup() {
  [[ -z "$gateway_port_forward_pid" ]] || kill "$gateway_port_forward_pid" >/dev/null 2>&1 || true
  [[ -z "$clickhouse_port_forward_pid" ]] || kill "$clickhouse_port_forward_pid" >/dev/null 2>&1 || true
  rm -rf "$temporary"
}
trap cleanup EXIT
curl_config="$temporary/clickhouse.curl"
install -m 0600 /dev/null "$curl_config"
{
  printf 'user = "molejo_reader:'
  kubectl --context "$context" -n molejo-observability get secret "$MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET" -o go-template='{{index .data "reader-password"}}' | base64 --decode
  printf '"\n'
} >"$curl_config"
kubectl --context "$context" -n molejo-observability port-forward service/otel-gateway 14318:4318 >"$temporary/port-forward.log" 2>&1 &
gateway_port_forward_pid=$!
kubectl --context "$context" -n molejo-observability port-forward service/clickhouse 18123:8123 >"$temporary/clickhouse-port-forward.log" 2>&1 &
clickhouse_port_forward_pid=$!
for _ in $(seq 1 30); do
  gateway_ready=false
  clickhouse_ready=false
  curl --silent --output /dev/null --max-time 1 http://127.0.0.1:14318/ && gateway_ready=true
  curl --silent --output /dev/null --max-time 1 --config "$curl_config" http://127.0.0.1:18123/ping && clickhouse_ready=true
  [[ "$gateway_ready" == true && "$clickhouse_ready" == true ]] && break
  kill -0 "$gateway_port_forward_pid" >/dev/null 2>&1 || { echo "OTLP port-forward terminated" >&2; exit 1; }
  kill -0 "$clickhouse_port_forward_pid" >/dev/null 2>&1 || { echo "ClickHouse port-forward terminated" >&2; exit 1; }
  sleep 1
done
[[ "$gateway_ready" == true && "$clickhouse_ready" == true ]] || { echo "observability port-forwards did not become ready" >&2; exit 1; }
marker="molejo-accept-$(openssl rand -hex 12)"
curl --fail --silent --show-error --max-time 10 \
  -H 'Content-Type: application/json' \
  --data "{\"resourceLogs\":[{\"scopeLogs\":[{\"logRecords\":[{\"body\":{\"stringValue\":\"$marker\"}}]}]}]}" \
  http://127.0.0.1:14318/v1/logs >/dev/null
observed=0
for _ in $(seq 1 30); do
  observed="$(curl --fail --silent --show-error --max-time 5 --config "$curl_config" --get \
    --data-urlencode "query=SELECT count() FROM otel.otel_logs WHERE Body = '$marker'" \
    http://127.0.0.1:18123/)"
  [[ "$observed" == "1" ]] && break
  sleep 1
done
[[ "$observed" == "1" ]] || { echo "OTLP acceptance record was not queryable in ClickHouse" >&2; exit 1; }

printf 'observability workloads, versioned configuration, and OTLP write/read path accepted on %s\n' "$context"
