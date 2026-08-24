#!/usr/bin/env bash
set -euo pipefail

for command in docker kubectl curl jq openssl go corepack node; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "$command is required for the control-plane E2E" >&2
    exit 2
  }
done

kind_cli() { go run sigs.k8s.io/kind@v0.32.0 "$@"; }

cluster_name="${FRUTO_KIND_CLUSTER:-fruto-control-plane-$PPID}"
if kind_cli get clusters 2>/dev/null | grep -Fxq "$cluster_name"; then
  echo "refusing to reuse existing Kind cluster $cluster_name" >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
kubeconfig="$tmp_dir/kubeconfig"
gateway_api_manifest="$tmp_dir/gateway-api.yaml"
api_binary="$tmp_dir/control-plane-api"
api_log="$tmp_dir/api.log"
vite_log="$tmp_dir/vite.log"
gateway_log="$tmp_dir/gateway-port-forward.log"
postgres_port="55432"
host_api_port="18080"
vite_port="5173"
cluster_created=false
compose_started=false
node_paused=false
host_api_pid=""
vite_pid=""
gateway_port_forward_pid=""
gateway_port=""
api_image="fruto-control-plane-api:local"
console_image="fruto-console-web:local"
operator_image="fruto-platform-operator:e2e"
fixture_image="fruto-control-plane-http-app:e2e"
node_name="$cluster_name-control-plane"

stop_pid() {
  local pid="${1:-}"
  [[ -z "$pid" ]] && return 0
  kill "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
}

cleanup() {
  local exit_code=$?
  [[ "$node_paused" == true ]] && docker unpause "$node_name" >/dev/null 2>&1 || true
  stop_pid "$host_api_pid"
  stop_pid "$vite_pid"
  stop_pid "$gateway_port_forward_pid"
  if [[ "$exit_code" -ne 0 && "$cluster_created" == true ]]; then
    [[ -f "$api_log" ]] && sed -n '1,160p' "$api_log" || true
    [[ -f "$vite_log" ]] && sed -n '1,160p' "$vite_log" || true
    kubectl --kubeconfig "$kubeconfig" get pods -A -o wide || true
    kubectl --kubeconfig "$kubeconfig" get events -A --sort-by=.lastTimestamp || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane logs deployment/control-plane-api --tail=120 || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane get jobs,pods,httproutes -o yaml || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-workspaces get appdeployments -o yaml || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-system logs deployment/platform-operator --tail=120 || true
  fi
  if [[ "$cluster_created" == true ]]; then
    kind_cli delete cluster --name "$cluster_name" >/dev/null 2>&1 || true
  fi
  if [[ "$compose_started" == true ]]; then
    docker compose -f deploy/control-plane/docker-compose.yaml down >/dev/null 2>&1 || true
  fi
  docker image rm "$api_image" "$console_image" "$operator_image" "$fixture_image" >/dev/null 2>&1 || true
  rm -rf "$tmp_dir"
  exit "$exit_code"
}
trap cleanup EXIT INT TERM

run_without_xtrace() {
  local tracing=0
  case $- in *x*) tracing=1; set +x;; esac
  "$@"
  local status=$?
  [[ "$tracing" -eq 1 ]] && set -x
  return "$status"
}

wait_http() {
  local url="$1"
  local expected="${2:-200}"
  for _ in $(seq 1 120); do
    local status
    status="$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 3 "$url" || true)"
    [[ "$status" == "$expected" ]] && return 0
    sleep 1
  done
  echo "timed out waiting for $url to return HTTP $expected" >&2
  return 1
}

start_host_api() {
  FRUTO_DATABASE_URL="postgres://fruto:fruto@127.0.0.1:${postgres_port}/fruto?sslmode=disable" \
    KUBECONFIG="$kubeconfig" \
    FRUTO_HTTP_ADDR="127.0.0.1:${host_api_port}" \
    FRUTO_ALLOWED_ORIGIN="http://127.0.0.1:${vite_port}" \
    FRUTO_COOKIE_SECURE=false \
    FRUTO_AUTO_MIGRATE=true \
    FRUTO_OPERATION_LEASE=2s \
    FRUTO_RUNTIME_TIMEOUT=1s \
    FRUTO_WORKSPACE_NAMESPACE=fruto-workspaces \
    "$api_binary" serve >"$api_log" 2>&1 &
  host_api_pid=$!
  wait_http "http://127.0.0.1:${host_api_port}/readyz"
}

start_vite() {
  VITE_API_PROXY_TARGET="http://127.0.0.1:${host_api_port}" \
    corepack pnpm --filter @fruto-platform/console-web dev --host 127.0.0.1 --port "$vite_port" >"$vite_log" 2>&1 &
  vite_pid=$!
  wait_http "http://127.0.0.1:${vite_port}/"
}

start_gateway_forward() {
  kubectl --kubeconfig "$kubeconfig" -n fruto-system port-forward svc/traefik-e2e :8443 >"$gateway_log" 2>&1 &
  gateway_port_forward_pid=$!
  for _ in $(seq 1 60); do
    gateway_port="$(sed -n 's/.*127\.0\.0\.1:\([0-9][0-9]*\).*/\1/p' "$gateway_log" | head -n 1)"
    [[ -n "$gateway_port" ]] && return 0
    sleep 0.2
  done
  cat "$gateway_log" >&2
  return 1
}

wait_operation() {
  local base_url="$1" operation_id="$2" cookie_jar="$3"
  for _ in $(seq 1 180); do
    local response status
    response="$(curl --silent --show-error -b "$cookie_jar" "$base_url/api/v1/operations/$operation_id")"
    status="$(jq -r '.status' <<<"$response")"
    case "$status" in
      Succeeded) return 0;;
      Failed|Superseded)
        echo "operation $operation_id ended as $status" >&2
        return 1
        ;;
    esac
    sleep 1
  done
  echo "timed out waiting for operation $operation_id" >&2
  return 1
}

prepare_owner_credentials() {
  owner_password="$(openssl rand -hex 24)"
  owner_hash="$(printf '%s' "$owner_password" | go run ./services/control-plane-api/cmd/control-plane-api hash-password)"
}

bootstrap_host_database() {
  FRUTO_DATABASE_URL="postgres://fruto:fruto@127.0.0.1:${postgres_port}/fruto?sslmode=disable" \
    FRUTO_OWNER_PASSWORD_HASH="$owner_hash" \
    FRUTO_WORKSPACE_NAMESPACE=fruto-workspaces \
    "$api_binary" bootstrap >/dev/null
}

login() {
  local base_url="$1" cookie_jar="$2"
  local payload response
  payload="$(jq -cn --arg password "$owner_password" '{actor:"owner",password:$password}')"
  response="$(curl --fail --silent --show-error -c "$cookie_jar" \
    -H "Origin: http://127.0.0.1:${vite_port}" -H 'Content-Type: application/json' \
    -d "$payload" "$base_url/api/v1/session")"
  csrf_token="$(jq -er '.csrfToken' <<<"$response")"
}

run_host_browser() {
  run_without_xtrace env \
    FRUTO_E2E_BASE_URL="http://127.0.0.1:${vite_port}" \
    FRUTO_E2E_IMAGE="$fixture_ref" \
    FRUTO_E2E_PASSWORD="$owner_password" \
    FRUTO_E2E_RESULTS="$tmp_dir/host-playwright.json" \
    FRUTO_E2E_OUTPUT_DIR="$tmp_dir/host-playwright" \
    corepack pnpm --filter @fruto-platform/console-web e2e
}

run_cluster_bootstrap() {
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/control-plane-api FRUTO_OWNER_PASSWORD_HASH="$owner_hash"
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane exec deployment/control-plane-api -- /control-plane-api bootstrap >/dev/null
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/control-plane-api FRUTO_OWNER_PASSWORD_HASH-
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
}

run_cluster_browser() {
  run_without_xtrace env \
    FRUTO_E2E_BASE_URL="$gateway_base_url" \
    FRUTO_E2E_HOST=console.fruto.calouro.tech \
    FRUTO_E2E_IMAGE="$fixture_ref" \
    FRUTO_E2E_PASSWORD="$owner_password" \
    FRUTO_E2E_RESULTS="$tmp_dir/in-cluster-playwright.json" \
    FRUTO_E2E_OUTPUT_DIR="$tmp_dir/in-cluster-playwright" \
    corepack pnpm --filter @fruto-platform/console-web e2e
}

assert_http_status() {
  local expected="$1" url="$2"; shift 2
  local status
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' "$@" "$url")"
  [[ "$status" == "$expected" ]] || { echo "expected HTTP $expected from $url, got $status" >&2; return 1; }
}

assert_rbac() {
  local identity="system:serviceaccount:fruto-control-plane:control-plane-api"
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get appdeployments.platform.fruto.calouro.tech -n fruto-workspaces --as="$identity")" == yes ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i patch appdeployments.platform.fruto.calouro.tech -n fruto-workspaces --as="$identity")" == yes ]]
  for resource in deployments.apps services httproutes.gateway.networking.k8s.io secrets pods; do
    [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i create "$resource" -n fruto-workspaces --as="$identity")" == no ]]
  done
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get namespaces/fruto-workspaces --as="$identity")" == yes ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get namespaces/kube-system --as="$identity")" == no ]]
}

run_concurrent_request() {
  local output="$1"
  curl --fail --silent --show-error -b "$host_cookie_jar" \
    -H 'Origin: http://127.0.0.1:5173' -H "X-CSRF-Token: $csrf_token" \
    -H 'Idempotency-Key: phase6-concurrent' -H 'Content-Type: application/json' \
    -d "$concurrent_intent" "http://127.0.0.1:${host_api_port}/api/v1/deployments" >"$output"
}

kind_cli create cluster --name "$cluster_name" --kubeconfig "$kubeconfig" --wait 120s
cluster_created=true

docker buildx build --file services/platform-operator/Dockerfile --tag "$operator_image" --load .
docker buildx build --file services/control-plane-api/Dockerfile --tag "$api_image" --load .
docker buildx build --file apps/console-web/Dockerfile --tag "$console_image" --load .
docker buildx build --file test/fixtures/http-app/Dockerfile --tag "$fixture_image" --build-arg VERSION=e2e-v1 --load .
kind_cli load docker-image --name "$cluster_name" "$operator_image" "$api_image" "$console_image" "$fixture_image"

curl -L --fail --silent --show-error \
  https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml \
  --output "$gateway_api_manifest"
echo "751002b3b91a87f7ae3bd2517c79a47a8d7ed6702901808a1cf9bd97d284f9b8  $gateway_api_manifest" | shasum -a 256 --check
kubectl --kubeconfig "$kubeconfig" apply --server-side -f "$gateway_api_manifest"
kubectl --kubeconfig "$kubeconfig" wait --for=condition=Established \
  crd/gateways.gateway.networking.k8s.io crd/httproutes.gateway.networking.k8s.io --timeout=60s

kubectl --kubeconfig "$kubeconfig" apply -f deploy/crds/platform.fruto.calouro.tech_appdeployments.yaml
kubectl --kubeconfig "$kubeconfig" apply -k deploy/operator
kubectl --kubeconfig "$kubeconfig" -n fruto-system set image deployment/platform-operator "manager=$operator_image"
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/platform-operator --timeout=120s
kubectl --kubeconfig "$kubeconfig" apply -f deploy/control-plane/namespace.yaml

fixture_full="docker.io/library/$fixture_image"
fixture_repository="${fixture_full%:*}"
fixture_digest="$(docker exec "$node_name" ctr --namespace=k8s.io images inspect "$fixture_full" | sed -n 's/.*@\(sha256:[a-f0-9]\{64\}\).*/\1/p' | head -n 1)"
[[ "$fixture_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "fixture digest was not resolved" >&2; exit 1; }
fixture_ref="$fixture_repository@$fixture_digest"
docker exec "$node_name" ctr --namespace=k8s.io images tag "$fixture_full" "$fixture_ref"

docker compose -f deploy/control-plane/docker-compose.yaml up -d postgres
compose_started=true
for _ in $(seq 1 60); do
  docker compose -f deploy/control-plane/docker-compose.yaml exec -T postgres pg_isready -U fruto -d fruto >/dev/null 2>&1 && break
  sleep 1
done
GOCACHE="$tmp_dir/go-cache" GOMODCACHE="${GOMODCACHE:-/tmp/fruto-go-mod-cache}" go build -o "$api_binary" ./services/control-plane-api/cmd/control-plane-api
run_without_xtrace prepare_owner_credentials
run_without_xtrace bootstrap_host_database
start_host_api
start_vite

host_cookie_jar="$tmp_dir/host-cookies.txt"
run_without_xtrace login http://127.0.0.1:${host_api_port} "$host_cookie_jar"
run_host_browser

intent="$(jq -cn --arg image "$fixture_ref" '{name:"phase6-api",image:$image,replicas:1,port:8080,resources:{requests:{cpuMillis:50,memoryMiB:64},limits:{cpuMillis:250,memoryMiB:128}},probes:{liveness:{path:"/healthz"},readiness:{path:"/readyz"}},exposure:"Private"}')"
create_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H 'Origin: http://127.0.0.1:5173' -H "X-CSRF-Token: $csrf_token" \
  -H 'Idempotency-Key: phase6-idempotent' -H 'Content-Type: application/json' \
  -d "$intent" "http://127.0.0.1:${host_api_port}/api/v1/deployments")"
api_deployment_id="$(jq -er '.deployment.id' <<<"$create_response")"
api_operation_id="$(jq -er '.operation.id' <<<"$create_response")"
repeat_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H 'Origin: http://127.0.0.1:5173' -H "X-CSRF-Token: $csrf_token" \
  -H 'Idempotency-Key: phase6-idempotent' -H 'Content-Type: application/json' \
  -d "$intent" "http://127.0.0.1:${host_api_port}/api/v1/deployments")"
[[ "$(jq -r '.operation.id' <<<"$repeat_response")" == "$api_operation_id" ]]
conflict_intent="$(jq '.name = "phase6-conflict"' <<<"$intent")"
assert_http_status 409 "http://127.0.0.1:${host_api_port}/api/v1/deployments" \
  -X POST -b "$host_cookie_jar" -H 'Origin: http://127.0.0.1:5173' \
  -H "X-CSRF-Token: $csrf_token" -H 'Idempotency-Key: phase6-idempotent' \
  -H 'Content-Type: application/json' -d "$conflict_intent"
assert_http_status 403 "http://127.0.0.1:${host_api_port}/api/v1/deployments" \
  -X POST -b "$host_cookie_jar" -H 'Origin: http://127.0.0.1:5173' \
  -H 'Idempotency-Key: phase6-no-csrf' -H 'Content-Type: application/json' -d "$intent"
assert_http_status 403 "http://127.0.0.1:${host_api_port}/api/v1/session" \
  -X POST -H 'Origin: https://invalid.example' -H 'Content-Type: application/json' \
  -d '{"actor":"owner","password":"invalid"}'
assert_http_status 404 "http://127.0.0.1:${host_api_port}/api/v1/deployments/dep-aaaaaaaaaaaaaaaaaaaa" -b "$host_cookie_jar"

wait_operation "http://127.0.0.1:${host_api_port}" "$api_operation_id" "$host_cookie_jar"
ready_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" "http://127.0.0.1:${host_api_port}/api/v1/deployments/$api_deployment_id")"
[[ "$(jq -r '.state' <<<"$ready_response")" == Ready ]]

kubectl --kubeconfig "$kubeconfig" scale deployment/platform-operator -n fruto-system --replicas=0
pending_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H 'Origin: http://127.0.0.1:5173' -H "X-CSRF-Token: $csrf_token" \
  -H 'Idempotency-Key: phase6-restart' -H 'Content-Type: application/json' \
  -d "$(jq '.name = "phase6-restart"' <<<"$intent")" "http://127.0.0.1:${host_api_port}/api/v1/deployments")"
pending_operation_id="$(jq -er '.operation.id' <<<"$pending_response")"
sleep 2
pending_status="$(curl --fail --silent --show-error -b "$host_cookie_jar" "http://127.0.0.1:${host_api_port}/api/v1/operations/$pending_operation_id" | jq -r '.status')"
[[ "$pending_status" == Pending || "$pending_status" == Running ]]
stop_pid "$host_api_pid"; host_api_pid=""
kubectl --kubeconfig "$kubeconfig" scale deployment/platform-operator -n fruto-system --replicas=1
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/platform-operator --timeout=120s
start_host_api
wait_operation "http://127.0.0.1:${host_api_port}" "$pending_operation_id" "$host_cookie_jar"

docker pause "$node_name"
node_paused=true
unknown_response="$(curl --fail --silent --show-error --max-time 8 -b "$host_cookie_jar" "http://127.0.0.1:${host_api_port}/api/v1/deployments/$api_deployment_id")"
[[ "$(jq -r '.state' <<<"$unknown_response")" == Unknown ]]
docker unpause "$node_name" >/dev/null
node_paused=false

concurrent_intent="$(jq '.name = "phase6-concurrent"' <<<"$intent")"
rm -f "$tmp_dir/concurrent-a.json" "$tmp_dir/concurrent-b.json"
run_without_xtrace run_concurrent_request "$tmp_dir/concurrent-a.json" &
concurrent_a_pid=$!
run_without_xtrace run_concurrent_request "$tmp_dir/concurrent-b.json" &
concurrent_b_pid=$!
wait "$concurrent_a_pid" "$concurrent_b_pid"
[[ "$(jq -r '.operation.id' "$tmp_dir/concurrent-a.json")" == "$(jq -r '.operation.id' "$tmp_dir/concurrent-b.json")" ]]
concurrent_operation_id="$(jq -r '.operation.id' "$tmp_dir/concurrent-a.json")"
concurrent_deployment_id="$(jq -r '.deployment.id' "$tmp_dir/concurrent-a.json")"
wait_operation "http://127.0.0.1:${host_api_port}" "$concurrent_operation_id" "$host_cookie_jar"
concurrent_runtime_name="ap-${concurrent_deployment_id#dep-}"
[[ "$(kubectl --kubeconfig "$kubeconfig" -n fruto-workspaces get appdeployment "$concurrent_runtime_name" -o name)" == "appdeployment.platform.fruto.calouro.tech/$concurrent_runtime_name" ]]

delete_api_deployment() {
  local deployment_id="$1" key="$2"
  local detail version response operation
  detail="$(curl --fail --silent --show-error -b "$host_cookie_jar" "http://127.0.0.1:${host_api_port}/api/v1/deployments/$deployment_id")"
  version="$(jq -r '.version' <<<"$detail")"
  response="$(curl --fail --silent --show-error -b "$host_cookie_jar" -X DELETE \
    -H 'Origin: http://127.0.0.1:5173' -H "X-CSRF-Token: $csrf_token" \
    -H "Idempotency-Key: $key" -H "If-Match: $version" \
    "http://127.0.0.1:${host_api_port}/api/v1/deployments/$deployment_id")"
  operation="$(jq -r '.operation.id' <<<"$response")"
  wait_operation "http://127.0.0.1:${host_api_port}" "$operation" "$host_cookie_jar"
}
run_without_xtrace delete_api_deployment "$api_deployment_id" phase6-delete-api
run_without_xtrace delete_api_deployment "$concurrent_deployment_id" phase6-delete-concurrent
stop_pid "$vite_pid"; vite_pid=""
stop_pid "$host_api_pid"; host_api_pid=""
docker compose -f deploy/control-plane/docker-compose.yaml down >/dev/null
compose_started=false

kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane create deployment postgres --image=postgres:17.6
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/postgres POSTGRES_USER=fruto POSTGRES_PASSWORD=fruto POSTGRES_DB=fruto
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane expose deployment postgres --port=5432 --name=postgres
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/postgres --timeout=120s
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane create secret generic fruto-control-plane-db --from-literal=database-url='postgres://fruto:fruto@postgres:5432/fruto?sslmode=disable'

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=*.fruto.calouro.tech' -addext 'subjectAltName=DNS:*.fruto.calouro.tech' -keyout "$tmp_dir/wildcard.key" -out "$tmp_dir/wildcard.crt" >/dev/null 2>&1
kubectl --kubeconfig "$kubeconfig" create namespace fruto-system 2>/dev/null || true
kubectl --kubeconfig "$kubeconfig" -n fruto-system create secret tls fruto-e2e-wildcard-tls --cert="$tmp_dir/wildcard.crt" --key="$tmp_dir/wildcard.key"
kubectl --kubeconfig "$kubeconfig" apply -f test/e2e/gateway.yaml
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/traefik-e2e --timeout=180s
kubectl --kubeconfig "$kubeconfig" apply -k deploy/control-plane-local
kubectl --kubeconfig "$kubeconfig" apply -f test/e2e/control-plane-gateway.yaml
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane wait --for=condition=complete job/control-plane-migrate --timeout=180s
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/console-web --timeout=120s
run_without_xtrace run_cluster_bootstrap
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
assert_rbac
start_gateway_forward
route_status=""
for _ in $(seq 1 60); do
  route_status="$(kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane get httproute/control-plane-console -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}' 2>/dev/null || true)"
  [[ "$route_status" == True ]] && break
  sleep 1
done
[[ "$route_status" == True ]]
gateway_base_url="https://console.fruto.calouro.tech:${gateway_port}"
gateway_curl_args=(--insecure --resolve "console.fruto.calouro.tech:${gateway_port}:127.0.0.1")
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/control-plane-api "FRUTO_ALLOWED_ORIGIN=${gateway_base_url}"
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
gateway_api_status=""
gateway_console_body=""
for _ in $(seq 1 60); do
  gateway_api_status="$(curl "${gateway_curl_args[@]}" --silent --output /dev/null --write-out '%{http_code}' "$gateway_base_url/api/v1/session" || true)"
  if [[ "$gateway_api_status" == 401 ]]; then
    gateway_console_body="$(curl "${gateway_curl_args[@]}" --silent "$gateway_base_url/deployments/dep-deep-link" || true)"
    grep -q '<div id="root">' <<<"$gateway_console_body" && break
  fi
  sleep 1
done
[[ "$gateway_api_status" == 401 ]]
grep -q '<div id="root">' <<<"$gateway_console_body"
run_cluster_browser

echo "control-plane Phase 6 E2E passed"
