#!/usr/bin/env bash
set -euo pipefail

trap 'echo "control-plane E2E failed at line $LINENO" >&2' ERR

for command in docker kubectl curl jq openssl go corepack node; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "$command is required for the control-plane E2E" >&2
    exit 2
  }
done

kind_cli() { go run sigs.k8s.io/kind@v0.32.0 "$@"; }

allocate_port() {
  node -e 'const net=require("net");const server=net.createServer();server.listen(0,"127.0.0.1",()=>{console.log(server.address().port);server.close();});'
}

cluster_name="${FRUTO_KIND_CLUSTER:-fruto-control-plane-$PPID}"
compose_project="fruto-control-plane-e2e-$PPID"
if kind_cli get clusters 2>/dev/null | grep -Fxq "$cluster_name"; then
  echo "refusing to reuse existing Kind cluster $cluster_name" >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
kubeconfig="$tmp_dir/kubeconfig"
gateway_api_manifest="$tmp_dir/gateway-api.yaml"
api_binary="$tmp_dir/control-plane-api"
api_log="$tmp_dir/api.log"
runtime_worker_log="$tmp_dir/runtime-worker.log"
vite_log="$tmp_dir/vite.log"
gateway_log="$tmp_dir/gateway-port-forward.log"
agent_log="$tmp_dir/agent-port-forward.log"
operator_manifest="$tmp_dir/operator.yaml"
agent_manifest="$tmp_dir/cluster-agent.yaml"
control_plane_manifest="$tmp_dir/control-plane.yaml"
postgres_port="$(allocate_port)"
host_api_port="$(allocate_port)"
while [[ "$host_api_port" == "$postgres_port" ]]; do host_api_port="$(allocate_port)"; done
vite_port="$(allocate_port)"
while [[ "$vite_port" == "$postgres_port" || "$vite_port" == "$host_api_port" ]]; do vite_port="$(allocate_port)"; done
runtime_timeout="1s"
cluster_created=false
compose_started=false
node_paused=false
api_image_built=false
console_image_built=false
operator_image_built=false
agent_image_built=false
fixture_image_built=false
host_api_pid=""
runtime_worker_pid=""
vite_pid=""
gateway_port_forward_pid=""
gateway_port=""
agent_port_forward_pid=""
agent_port=""
api_image="fruto-control-plane-api:local-$PPID"
console_image="fruto-console-web:local-$PPID"
operator_image="fruto-platform-operator:e2e-$PPID"
agent_image="molejo-cluster-agent:e2e-$PPID"
fixture_image="fruto-control-plane-http-app:e2e-$PPID"
node_name="$cluster_name-control-plane"

stop_pid() {
  local pid="${1:-}"
  [[ -z "$pid" ]] && return 0
  kill "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
}

cleanup() {
  local exit_code=$?
  local cleanup_failed=false
  if [[ "$node_paused" == true ]] && ! docker unpause "$node_name" >/dev/null 2>&1; then
    cleanup_failed=true
  fi
  stop_pid "$host_api_pid"
  stop_pid "$runtime_worker_pid"
  stop_pid "$vite_pid"
  stop_pid "$gateway_port_forward_pid"
  stop_pid "$agent_port_forward_pid"
  if [[ "$exit_code" -ne 0 && "$cluster_created" == true ]]; then
    [[ -f "$api_log" ]] && sed -n '1,160p' "$api_log" || true
    [[ -f "$runtime_worker_log" ]] && sed -n '1,160p' "$runtime_worker_log" || true
    [[ -f "$vite_log" ]] && sed -n '1,160p' "$vite_log" || true
    [[ -f "$agent_log" ]] && sed -n '1,160p' "$agent_log" || true
    kubectl --kubeconfig "$kubeconfig" get pods -A -o wide || true
    kubectl --kubeconfig "$kubeconfig" get events -A --sort-by=.lastTimestamp || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane logs deployment/control-plane-api --tail=120 || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane get jobs,httproutes -o yaml || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-workspaces get appdeployments -o yaml || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-system logs deployment/platform-operator --tail=120 || true
    kubectl --kubeconfig "$kubeconfig" -n fruto-system logs deployment/cluster-agent --tail=120 || true
  fi
  if [[ "$cluster_created" == true ]]; then
    if ! kind_cli delete cluster --name "$cluster_name" >/dev/null 2>&1; then
      cleanup_failed=true
    fi
  fi
  if [[ "$compose_started" == true ]]; then
    if ! FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f deploy/control-plane/docker-compose.yaml down >/dev/null 2>&1; then
      cleanup_failed=true
    fi
  fi
  if [[ "$api_image_built" == true ]] && ! docker image rm "$api_image" >/dev/null 2>&1; then cleanup_failed=true; fi
  if [[ "$console_image_built" == true ]] && ! docker image rm "$console_image" >/dev/null 2>&1; then cleanup_failed=true; fi
  if [[ "$operator_image_built" == true ]] && ! docker image rm "$operator_image" >/dev/null 2>&1; then cleanup_failed=true; fi
  if [[ "$agent_image_built" == true ]] && ! docker image rm "$agent_image" >/dev/null 2>&1; then cleanup_failed=true; fi
  if [[ "$fixture_image_built" == true ]] && ! docker image rm "$fixture_image" >/dev/null 2>&1; then cleanup_failed=true; fi
  if ! rm -rf "$tmp_dir"; then
    cleanup_failed=true
  fi
  if [[ "$exit_code" -eq 0 && "$cleanup_failed" == true ]]; then
    exit_code=1
  fi
  trap - EXIT INT TERM
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
    FRUTO_EXPECTED_KUBE_CONTEXT="$expected_kube_context" \
    FRUTO_EXPECTED_KUBE_SERVER="$expected_kube_server" \
    FRUTO_EXPECTED_CLUSTER_UID="$expected_cluster_uid" \
    FRUTO_HTTP_ADDR="127.0.0.1:${host_api_port}" \
    FRUTO_MODE=development \
    FRUTO_PUBLIC_URL="http://127.0.0.1:${vite_port}" \
    FRUTO_ALLOWED_ORIGIN="http://127.0.0.1:${vite_port}" \
    FRUTO_ALLOWED_HOSTS="127.0.0.1:${host_api_port},127.0.0.1:${vite_port}" \
    FRUTO_ALLOWED_REGISTRIES=docker.io \
    FRUTO_COOKIE_SECURE=false \
    FRUTO_AUTO_MIGRATE=true \
    FRUTO_OPERATION_LEASE=2s \
    FRUTO_RUNTIME_TIMEOUT="$runtime_timeout" \
    FRUTO_WORKSPACE_NAMESPACE=fruto-workspaces \
    "$api_binary" serve >"$api_log" 2>&1 &
  host_api_pid=$!
  wait_http "http://127.0.0.1:${host_api_port}/readyz"
}

start_host_runtime_worker() {
  FRUTO_DATABASE_URL="postgres://fruto:fruto@127.0.0.1:${postgres_port}/fruto?sslmode=disable" \
    KUBECONFIG="$kubeconfig" \
    FRUTO_EXPECTED_KUBE_CONTEXT="$expected_kube_context" \
    FRUTO_EXPECTED_KUBE_SERVER="$expected_kube_server" \
    FRUTO_EXPECTED_CLUSTER_UID="$expected_cluster_uid" \
    FRUTO_OPERATION_LEASE=2s \
  FRUTO_RUNTIME_WORKER_ID=e2e-runtime-worker \
    "$api_binary" runtime-worker >"$runtime_worker_log" 2>&1 &
  runtime_worker_pid=$!
  sleep 0.2
  kill -0 "$runtime_worker_pid" 2>/dev/null || { cat "$runtime_worker_log" >&2; return 1; }
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

start_agent_forward() {
  kubectl --kubeconfig "$kubeconfig" -n fruto-system port-forward deployment/cluster-agent :8081 >"$agent_log" 2>&1 &
  agent_port_forward_pid=$!
  for _ in $(seq 1 60); do
    agent_port="$(sed -n 's/.*127\.0\.0\.1:\([0-9][0-9]*\).*/\1/p' "$agent_log" | head -n 1)"
    [[ -n "$agent_port" ]] && return 0
    sleep 0.2
  done
  cat "$agent_log" >&2
  return 1
}

wait_agent_state() {
  local expected="$1"
  for _ in $(seq 1 120); do
    local state
    state="$(curl --silent --max-time 2 "http://127.0.0.1:${agent_port}/status" | jq -r '.state // empty' || true)"
    [[ "$state" == "$expected" ]] && return 0
    sleep 1
  done
  echo "timed out waiting for Agent state $expected" >&2
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
  payload="$(jq -cn --arg password "$owner_password" '{username:"owner",password:$password}')"
  response="$(curl --fail --silent --show-error -c "$cookie_jar" \
    -H "Origin: http://127.0.0.1:${vite_port}" -H 'Content-Type: application/json' \
    -d "$payload" "$base_url/api/v1/session")"
  csrf_token="$(jq -er '.csrfToken' <<<"$response")"
}

run_cluster_bootstrap() {
  local owner_hash_file="$tmp_dir/owner-password-hash"
  printf '%s' "$owner_hash" >"$owner_hash_file"
  chmod 600 "$owner_hash_file"
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane create secret generic control-plane-bootstrap-owner --from-file=FRUTO_OWNER_PASSWORD_HASH="$owner_hash_file"
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/control-plane-api --from=secret/control-plane-bootstrap-owner
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane exec deployment/control-plane-api -- /control-plane-api bootstrap >/dev/null
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/control-plane-api FRUTO_OWNER_PASSWORD_HASH-
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
  kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane delete secret control-plane-bootstrap-owner
}

run_cluster_browser() {
  run_without_xtrace env \
    FRUTO_E2E_BASE_URL="$gateway_base_url" \
    FRUTO_E2E_HOST=cloud.molejo.dev \
    FRUTO_E2E_ALLOW_UNTRUSTED_TLS=true \
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
  local api_identity="system:serviceaccount:fruto-control-plane:control-plane-api"
  local worker_identity="system:serviceaccount:fruto-control-plane:control-plane-runtime-worker"
  local parameter_worker_identity="system:serviceaccount:fruto-control-plane:control-plane-parameter-worker"
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get appdeployments.platform.fruto.calouro.tech -n fruto-workspaces --as="$api_identity")" == no ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get secrets -n fruto-workspaces --as="$api_identity")" == no ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get appdeployments.platform.fruto.calouro.tech -n fruto-workspaces --as="$worker_identity")" == yes ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i patch appdeployments.platform.fruto.calouro.tech -n fruto-workspaces --as="$worker_identity")" == yes ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i create secrets -n fruto-workspaces --as="$worker_identity")" == yes ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get deployments.apps -n fruto-workspaces --as="$worker_identity")" == no ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get secrets -n fruto-control-plane --as="$worker_identity")" == no ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get secrets -n fruto-workspaces --as="$parameter_worker_identity")" == no ]]
}

assert_agent_rbac() {
  local agent_identity="system:serviceaccount:fruto-system:cluster-agent"
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get secret/molejo-agent-identity -n fruto-system --as="$agent_identity")" == yes ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i patch secret/molejo-agent-enrollment -n fruto-system --as="$agent_identity")" == yes ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i list secrets -n fruto-system --as="$agent_identity")" == no ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get secret/fruto-e2e-wildcard-tls -n fruto-system --as="$agent_identity")" == no ]]
  [[ "$(kubectl --kubeconfig "$kubeconfig" auth can-i get appdeployments.platform.fruto.calouro.tech -n fruto-workspaces --as="$agent_identity")" == no ]]
}

run_concurrent_request() {
  local output="$1"
  curl --fail --silent --show-error -b "$host_cookie_jar" \
    -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
    -H "If-Match: $app_environment_version" \
    -H 'Idempotency-Key: e2e-concurrent' -H 'Content-Type: application/json' \
    -d "$deployment_intent" "$deployment_base" >"$output"
}

seed_test_release() {
  FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" \
    -f deploy/control-plane/docker-compose.yaml exec -T postgres \
    psql -U fruto -d fruto -v ON_ERROR_STOP=1 \
      -v workspace_id="$workspace_id" -v project_id="$project_id" \
      -v app_id="$app_id" -v app_environment_id="$app_environment_id" \
      -v fixture_ref="$fixture_ref" -v release_id="$release_id" >/dev/null <<'SQL'
WITH refs AS (
  SELECT w.id AS workspace_id, p.id AS project_id, a.id AS app_id,
         ae.id AS app_environment_id, wm.user_id
  FROM workspaces w
  JOIN projects p ON p.workspace_id=w.id
  JOIN apps a ON a.project_id=p.id
  JOIN app_environments ae ON ae.app_id=a.id
  JOIN workspace_memberships wm ON wm.workspace_id=w.id
  WHERE w.public_id=:'workspace_id' AND p.public_id=:'project_id'
    AND a.public_id=:'app_id' AND ae.public_id=:'app_environment_id'
  LIMIT 1
), inserted_build AS (
  INSERT INTO builds(
    public_id,workspace_id,project_id,app_id,app_environment_id,
    requested_by_user_id,github_installation_external_id,repository_id,
    repository_full_name,source_branch,commit_sha,platform,status,
    idempotency_hash,payload_hash,completed_at
  )
  SELECT 'bld-aaaaaaaaaaaaaaaaaaaa',workspace_id,project_id,app_id,
    app_environment_id,user_id,1,1,'molejo/e2e','main',repeat('a',40),
    'linux/amd64','Succeeded',decode(repeat('01',32),'hex'),
    decode(repeat('02',32),'hex'),now()
  FROM refs
  RETURNING id,workspace_id,project_id,app_id,app_environment_id
)
INSERT INTO releases(public_id,workspace_id,project_id,app_id,app_environment_id,build_id,commit_sha,image,platform)
SELECT :'release_id',workspace_id,project_id,app_id,app_environment_id,id,repeat('a',40),:'fixture_ref','linux/amd64'
FROM inserted_build;
SQL
}

kind_cli create cluster --name "$cluster_name" --kubeconfig "$kubeconfig" --wait 120s
cluster_created=true
expected_kube_context="$(kubectl --kubeconfig "$kubeconfig" config current-context)"
expected_kube_server="$(kubectl --kubeconfig "$kubeconfig" config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
expected_cluster_uid="$(kubectl --kubeconfig "$kubeconfig" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ -n "$expected_kube_context" && -n "$expected_kube_server" && -n "$expected_cluster_uid" ]]

docker buildx build --file services/platform-operator/Dockerfile --tag "$operator_image" --load .
operator_image_built=true
docker buildx build --file services/cluster-agent/Dockerfile --tag "$agent_image" --load .
agent_image_built=true
docker buildx build --file services/control-plane-api/Dockerfile --tag "$api_image" --load .
api_image_built=true
docker buildx build --file apps/console-web/Dockerfile --tag "$console_image" --load .
console_image_built=true
docker buildx build --file test/fixtures/http-app/Dockerfile --tag "$fixture_image" --build-arg VERSION=e2e-v1 --load .
fixture_image_built=true
kind_cli load docker-image --name "$cluster_name" "$operator_image" "$agent_image" "$api_image" "$console_image" "$fixture_image"

curl -L --fail --silent --show-error \
  https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/experimental-install.yaml \
  --output "$gateway_api_manifest"
echo "64ec76609a6ac885e0405dea79ca509c229fa019d342f0857aa8b6bdc8b8ba92  $gateway_api_manifest" | shasum -a 256 --check
kubectl --kubeconfig "$kubeconfig" apply --server-side -f "$gateway_api_manifest"
kubectl --kubeconfig "$kubeconfig" wait --for=condition=Established \
  crd/gateways.gateway.networking.k8s.io crd/httproutes.gateway.networking.k8s.io crd/tcproutes.gateway.networking.k8s.io --timeout=60s

kubectl --kubeconfig "$kubeconfig" apply -k deploy/crds
kubectl --kubeconfig "$kubeconfig" wait --for=condition=Established \
  crd/appdeployments.platform.fruto.calouro.tech \
  crd/appvolumes.platform.fruto.calouro.tech \
  --timeout=60s
kubectl kustomize deploy/operator |
  sed "s|image: ghcr.io/fruto-platform/platform-operator@sha256:0000000000000000000000000000000000000000000000000000000000000000|image: $operator_image|" >"$operator_manifest"
grep -Fq "image: $operator_image" "$operator_manifest"
kubectl --kubeconfig "$kubeconfig" apply -f "$operator_manifest"
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/platform-operator --timeout=120s
kubectl kustomize deploy/cluster-agent |
  sed "s|image: ghcr.io/fruto-platform/cluster-agent@sha256:0000000000000000000000000000000000000000000000000000000000000000|image: $agent_image|" >"$agent_manifest"
grep -Fq "image: $agent_image" "$agent_manifest"
kubectl --kubeconfig "$kubeconfig" apply -f "$agent_manifest"
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/cluster-agent --timeout=120s
assert_agent_rbac
start_agent_forward
wait_agent_state Unpaired
kubectl --kubeconfig "$kubeconfig" apply -f deploy/control-plane/namespace.yaml

fixture_full="docker.io/library/$fixture_image"
fixture_repository="${fixture_full%:*}"
fixture_digest="$(docker exec "$node_name" ctr --namespace=k8s.io images inspect "$fixture_full" | sed -n 's/.*@\(sha256:[a-f0-9]\{64\}\).*/\1/p' | head -n 1)"
[[ "$fixture_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "fixture digest was not resolved" >&2; exit 1; }
fixture_ref="$fixture_repository@$fixture_digest"
docker exec "$node_name" ctr --namespace=k8s.io images tag "$fixture_full" "$fixture_ref"

FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f deploy/control-plane/docker-compose.yaml up -d postgres
compose_started=true
postgres_ready=false
for _ in $(seq 1 60); do
  if FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f deploy/control-plane/docker-compose.yaml exec -T postgres pg_isready -U fruto -d fruto >/dev/null 2>&1; then
    postgres_ready=true
    break
  fi
  sleep 1
done
if [[ "$postgres_ready" != true ]]; then
  FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f deploy/control-plane/docker-compose.yaml logs postgres >&2
  exit 1
fi
GOCACHE="$tmp_dir/go-cache" GOMODCACHE="${GOMODCACHE:-/tmp/fruto-go-mod-cache}" go build -o "$api_binary" ./services/control-plane-api/cmd/control-plane-api
run_without_xtrace prepare_owner_credentials
run_without_xtrace bootstrap_host_database
start_host_api
start_host_runtime_worker
start_vite

host_cookie_jar="$tmp_dir/host-cookies.txt"
echo "validating host control-plane API"
run_without_xtrace login http://127.0.0.1:${host_api_port} "$host_cookie_jar"

api_base="http://127.0.0.1:${host_api_port}/api/v1"
workspace_id="$(curl --fail --silent --show-error -b "$host_cookie_jar" "$api_base/workspaces/current" | jq -er '.id')"
project_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
  -H 'Content-Type: application/json' -d '{"name":"E2E Project"}' \
  "$api_base/workspaces/$workspace_id/projects")"
project_id="$(jq -er '.id' <<<"$project_response")"
environment_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
  -H 'Content-Type: application/json' -d '{"name":"Development"}' \
  "$api_base/workspaces/$workspace_id/projects/$project_id/environments")"
environment_id="$(jq -er '.id' <<<"$environment_response")"
app_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
  -H 'Content-Type: application/json' -d '{"name":"E2E App"}' \
  "$api_base/workspaces/$workspace_id/projects/$project_id/apps")"
app_id="$(jq -er '.id' <<<"$app_response")"
configuration="$(jq -cn --arg environment "$environment_id" '{environmentId:$environment,branch:"main",workloadKind:"Stateless",configuration:{replicas:1,ports:[{name:"http",containerPort:8080,protocol:"TCP"}],resources:{requests:{cpuMillis:50,memoryMiB:64},limits:{cpuMillis:250,memoryMiB:128}},probes:{startup:{type:"HTTP",portName:"http",path:"/readyz"},liveness:{type:"HTTP",portName:"http",path:"/healthz"},readiness:{type:"HTTP",portName:"http",path:"/readyz"}},publicEndpoints:[],variables:[],parameters:[]}}')"
app_environment_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
  -H 'Content-Type: application/json' -d "$configuration" \
  "$api_base/workspaces/$workspace_id/projects/$project_id/apps/$app_id/environments")"
app_environment_id="$(jq -er '.id' <<<"$app_environment_response")"
echo "host hierarchy created"
app_environment_version="$(jq -er '.version' <<<"$app_environment_response")"
configuration_version="$(jq -er '.configurationVersion' <<<"$app_environment_response")"
app_environment_base="$api_base/workspaces/$workspace_id/projects/$project_id/apps/$app_id/environments/$app_environment_id"
deployment_base="$app_environment_base/deployments"
release_id="rel-bbbbbbbbbbbbbbbbbbbb"
run_without_xtrace seed_test_release
deployment_intent="$(jq -cn --arg release "$release_id" --argjson configuration "$configuration_version" '{releaseId:$release,configurationVersion:$configuration,currentDeploymentId:null}')"
create_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
  -H "If-Match: $app_environment_version" \
  -H 'Idempotency-Key: e2e-idempotent' -H 'Content-Type: application/json' \
  -d "$deployment_intent" "$deployment_base")"
api_deployment_id="$(jq -er '.deployment.id' <<<"$create_response")"
api_operation_id="$(jq -er '.operation.id' <<<"$create_response")"
repeat_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
  -H "If-Match: $app_environment_version" \
  -H 'Idempotency-Key: e2e-idempotent' -H 'Content-Type: application/json' \
  -d "$deployment_intent" "$deployment_base")"
[[ "$(jq -r '.operation.id' <<<"$repeat_response")" == "$api_operation_id" ]]
conflict_intent="$(jq -cn --argjson configuration "$configuration_version" '{releaseId:"rel-cccccccccccccccccccc",configurationVersion:$configuration,currentDeploymentId:null}')"
assert_http_status 409 "$deployment_base" \
  -X POST -b "$host_cookie_jar" -H "Origin: http://127.0.0.1:${vite_port}" \
  -H "X-CSRF-Token: $csrf_token" -H 'Idempotency-Key: e2e-idempotent' \
  -H "If-Match: $app_environment_version" \
  -H 'Content-Type: application/json' -d "$conflict_intent"
assert_http_status 403 "$deployment_base" \
  -X POST -b "$host_cookie_jar" -H "Origin: http://127.0.0.1:${vite_port}" \
  -H "If-Match: $app_environment_version" \
  -H 'Idempotency-Key: e2e-no-csrf' -H 'Content-Type: application/json' -d "$deployment_intent"
assert_http_status 403 "http://127.0.0.1:${host_api_port}/api/v1/session" \
  -X POST -H 'Origin: https://invalid.example' -H 'Content-Type: application/json' \
  -d '{"username":"owner","password":"invalid"}'
assert_http_status 404 "$deployment_base/dpl-aaaaaaaaaaaaaaaaaaaa" -b "$host_cookie_jar"

wait_operation "http://127.0.0.1:${host_api_port}" "$api_operation_id" "$host_cookie_jar"
echo "host deployment reached Ready"
ready_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" "$deployment_base/$api_deployment_id")"
[[ "$(jq -r '.state' <<<"$ready_response")" == Ready ]]
app_environment_detail="$(curl --fail --silent --show-error -b "$host_cookie_jar" "$app_environment_base")"
app_environment_version="$(jq -er '.version' <<<"$app_environment_detail")"
current_deployment_id="$(jq -er '.currentDeploymentId' <<<"$app_environment_detail")"
deployment_intent="$(jq -cn --arg release "$release_id" --arg current "$current_deployment_id" --argjson configuration "$configuration_version" '{releaseId:$release,configurationVersion:$configuration,currentDeploymentId:$current}')"

kubectl --kubeconfig "$kubeconfig" scale deployment/platform-operator -n fruto-system --replicas=0
stop_pid "$host_api_pid"; host_api_pid=""
stop_pid "$runtime_worker_pid"; runtime_worker_pid=""
runtime_timeout="30s"
start_host_api
start_host_runtime_worker
docker pause "$node_name" >/dev/null
node_paused=true
pending_response="$(curl --fail --silent --show-error -b "$host_cookie_jar" \
  -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
  -H "If-Match: $app_environment_version" \
  -H 'Idempotency-Key: e2e-restart' -H 'Content-Type: application/json' \
  -d "$deployment_intent" "$deployment_base")"
pending_operation_id="$(jq -er '.operation.id' <<<"$pending_response")"
pending_status=""
for _ in $(seq 1 100); do
  pending_status="$(curl --fail --silent --show-error -b "$host_cookie_jar" "http://127.0.0.1:${host_api_port}/api/v1/operations/$pending_operation_id" | jq -r '.status')"
  [[ "$pending_status" == Running ]] && break
  sleep 0.1
done
[[ "$pending_status" == Running ]]
kill -KILL "$runtime_worker_pid"
wait "$runtime_worker_pid" >/dev/null 2>&1 || true
runtime_worker_pid=""
stop_pid "$host_api_pid"; host_api_pid=""
docker unpause "$node_name" >/dev/null
node_paused=false
runtime_timeout="1s"
kubectl --kubeconfig "$kubeconfig" scale deployment/platform-operator -n fruto-system --replicas=1
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/platform-operator --timeout=120s
start_host_api
start_host_runtime_worker
wait_operation "http://127.0.0.1:${host_api_port}" "$pending_operation_id" "$host_cookie_jar"

docker pause "$node_name"
node_paused=true
unknown_response="$(curl --fail --silent --show-error --max-time 8 -b "$host_cookie_jar" "$app_environment_base")"
[[ "$(jq -r '.state' <<<"$unknown_response")" == Unknown ]]
docker unpause "$node_name" >/dev/null
node_paused=false

app_environment_detail="$(curl --fail --silent --show-error -b "$host_cookie_jar" "$app_environment_base")"
app_environment_version="$(jq -er '.version' <<<"$app_environment_detail")"
current_deployment_id="$(jq -er '.currentDeploymentId' <<<"$app_environment_detail")"
deployment_intent="$(jq -cn --arg release "$release_id" --arg current "$current_deployment_id" --argjson configuration "$configuration_version" '{releaseId:$release,configurationVersion:$configuration,currentDeploymentId:$current}')"

rm -f "$tmp_dir/concurrent-a.json" "$tmp_dir/concurrent-b.json"
run_without_xtrace run_concurrent_request "$tmp_dir/concurrent-a.json" &
concurrent_a_pid=$!
run_without_xtrace run_concurrent_request "$tmp_dir/concurrent-b.json" &
concurrent_b_pid=$!
wait "$concurrent_a_pid" "$concurrent_b_pid"
[[ "$(jq -r '.operation.id' "$tmp_dir/concurrent-a.json")" == "$(jq -r '.operation.id' "$tmp_dir/concurrent-b.json")" ]]
concurrent_operation_id="$(jq -r '.operation.id' "$tmp_dir/concurrent-a.json")"
wait_operation "http://127.0.0.1:${host_api_port}" "$concurrent_operation_id" "$host_cookie_jar"
concurrent_runtime_name="ap-$app_environment_id"
[[ "$(kubectl --kubeconfig "$kubeconfig" -n fruto-workspaces get appdeployment "$concurrent_runtime_name" -o name)" == "appdeployment.platform.fruto.calouro.tech/$concurrent_runtime_name" ]]

delete_app_environment() {
  local detail version response operation
  detail="$(curl --fail --silent --show-error -b "$host_cookie_jar" "$app_environment_base")"
  version="$(jq -r '.version' <<<"$detail")"
  response="$(curl --fail --silent --show-error -b "$host_cookie_jar" -X DELETE \
    -H "Origin: http://127.0.0.1:${vite_port}" -H "X-CSRF-Token: $csrf_token" \
    -H 'Idempotency-Key: e2e-delete-app-environment' -H "If-Match: $version" \
    "$app_environment_base")"
  operation="$(jq -r '.id' <<<"$response")"
  wait_operation "http://127.0.0.1:${host_api_port}" "$operation" "$host_cookie_jar"
}
run_without_xtrace delete_app_environment
stop_pid "$vite_pid"; vite_pid=""
stop_pid "$host_api_pid"; host_api_pid=""
stop_pid "$runtime_worker_pid"; runtime_worker_pid=""
FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f deploy/control-plane/docker-compose.yaml down >/dev/null
compose_started=false

kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane create deployment postgres --image=postgres:17.6
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/postgres POSTGRES_USER=fruto POSTGRES_PASSWORD=fruto POSTGRES_DB=fruto
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane expose deployment postgres --port=5432 --name=postgres
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/postgres --timeout=120s
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane create secret generic fruto-control-plane-db --from-literal=database-url='postgres://fruto:fruto@postgres:5432/fruto?sslmode=disable'

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=*.molejo.dev' -addext 'subjectAltName=DNS:*.molejo.dev' -keyout "$tmp_dir/wildcard.key" -out "$tmp_dir/wildcard.crt" >/dev/null 2>&1
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 \
  -subj '/CN=Molejo Agent E2E CA' -keyout "$tmp_dir/agent-ca.key" -out "$tmp_dir/agent-ca.crt" >/dev/null 2>&1
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -subj '/CN=control-plane-api.fruto-control-plane.svc.cluster.local' \
  -addext 'subjectAltName=DNS:control-plane-api.fruto-control-plane.svc.cluster.local' \
  -addext 'extendedKeyUsage=serverAuth' \
  -keyout "$tmp_dir/agent-server.key" -out "$tmp_dir/agent-server.csr" >/dev/null 2>&1
openssl x509 -req -days 1 -sha256 -copy_extensions copy \
  -in "$tmp_dir/agent-server.csr" -CA "$tmp_dir/agent-ca.crt" -CAkey "$tmp_dir/agent-ca.key" -CAcreateserial \
  -out "$tmp_dir/agent-server.crt" >/dev/null 2>&1
kubectl --kubeconfig "$kubeconfig" create namespace fruto-system 2>/dev/null || true
kubectl --kubeconfig "$kubeconfig" -n fruto-system create secret tls fruto-e2e-wildcard-tls --cert="$tmp_dir/wildcard.crt" --key="$tmp_dir/wildcard.key"
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane create secret generic required-external-agent-ca-secret \
  --from-file=ca.crt="$tmp_dir/agent-ca.crt" --from-file=ca.key="$tmp_dir/agent-ca.key"
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane create secret tls required-external-agent-server-tls \
  --cert="$tmp_dir/agent-server.crt" --key="$tmp_dir/agent-server.key"
kubectl --kubeconfig "$kubeconfig" apply -f test/e2e/gateway.yaml
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/traefik-e2e --timeout=180s
gateway_cluster_ip="$(kubectl --kubeconfig "$kubeconfig" -n fruto-system get service traefik-e2e -o jsonpath='{.spec.clusterIP}')"
[[ -n "$gateway_cluster_ip" && "$gateway_cluster_ip" != None ]]
kubectl --kubeconfig "$kubeconfig" -n fruto-system create configmap agent-enrollment-ca --from-file=ca.crt="$tmp_dir/wildcard.crt"
kubectl --kubeconfig "$kubeconfig" -n fruto-system patch configmap cluster-agent-config --type merge \
  -p '{"data":{"MOLEJO_AGENT_ENROLLMENT_URL":"https://cloud.molejo.dev:8443/agent/v1/enroll"}}'
agent_patch="$(jq -cn --arg ip "$gateway_cluster_ip" '{spec:{template:{spec:{hostAliases:[{ip:$ip,hostnames:["cloud.molejo.dev"]}],containers:[{name:"agent",env:[{name:"MOLEJO_AGENT_ENROLLMENT_CA_FILE",value:"/var/run/secrets/molejo/enrollment-ca/ca.crt"}],volumeMounts:[{name:"enrollment-ca",mountPath:"/var/run/secrets/molejo/enrollment-ca",readOnly:true}]}],volumes:[{name:"enrollment-ca",configMap:{name:"agent-enrollment-ca"}}]}}}}')"
kubectl --kubeconfig "$kubeconfig" -n fruto-system patch deployment cluster-agent --type strategic -p "$agent_patch"
kubectl --kubeconfig "$kubeconfig" -n fruto-system rollout status deployment/cluster-agent --timeout=120s
stop_pid "$agent_port_forward_pid"; agent_port_forward_pid=""; agent_port=""
start_agent_forward
wait_agent_state Unpaired
kubectl kustomize deploy/control-plane-local |
  sed -e "s|image: fruto-control-plane-api:local|image: $api_image|g" \
    -e "s|image: fruto-console-web:local|image: $console_image|g" \
    -e "s|required-external-cluster-uid|$expected_cluster_uid|g" \
    -e "s|required-external-proxy-cidr|0.0.0.0/0|g" >"$control_plane_manifest"
grep -Fq "image: $api_image" "$control_plane_manifest"
grep -Fq "image: $console_image" "$control_plane_manifest"
grep -Fq "FRUTO_EXPECTED_CLUSTER_UID: $expected_cluster_uid" "$control_plane_manifest"
kubectl --kubeconfig "$kubeconfig" apply -f "$control_plane_manifest"
kubectl --kubeconfig "$kubeconfig" apply -f test/e2e/control-plane-gateway.yaml
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane wait --for=condition=complete job/control-plane-migrate --timeout=180s
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/console-web --timeout=120s
echo "validating in-cluster control plane"
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
gateway_base_url="https://cloud.molejo.dev:${gateway_port}"
gateway_curl_args=(--insecure --resolve "cloud.molejo.dev:${gateway_port}:127.0.0.1")
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane set env deployment/control-plane-api \
  "FRUTO_PUBLIC_URL=${gateway_base_url}" \
  "FRUTO_ALLOWED_ORIGIN=${gateway_base_url}" \
  "FRUTO_ALLOWED_HOSTS=cloud.molejo.dev,cloud.molejo.dev:8443,cloud.molejo.dev:${gateway_port}" \
  "FRUTO_AGENT_CA_CERT_FILE=/var/run/secrets/molejo/agent-ca/ca.crt" \
  "FRUTO_AGENT_CA_KEY_FILE=/var/run/secrets/molejo/agent-ca/ca.key" \
  "FRUTO_AGENT_SERVER_CERT_FILE=/var/run/secrets/molejo/agent-server/tls.crt" \
  "FRUTO_AGENT_SERVER_KEY_FILE=/var/run/secrets/molejo/agent-server/tls.key"
kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=120s
gateway_api_status=""
gateway_console_body=""
for _ in $(seq 1 60); do
  gateway_api_status="$(curl "${gateway_curl_args[@]}" --silent --output /dev/null --write-out '%{http_code}' "$gateway_base_url/api/v1/session" || true)"
  if [[ "$gateway_api_status" == 401 ]]; then
    gateway_console_body="$(curl "${gateway_curl_args[@]}" --silent "$gateway_base_url/workspaces/example/projects" || true)"
    grep -q '<div id="root">' <<<"$gateway_console_body" && break
  fi
  sleep 1
done
[[ "$gateway_api_status" == 401 ]]
grep -q '<div id="root">' <<<"$gateway_console_body"

agent_cookie_jar="$tmp_dir/agent-pairing-cookies.txt"
agent_login_payload="$(jq -cn --arg password "$owner_password" '{username:"owner",password:$password}')"
agent_login_response="$(curl "${gateway_curl_args[@]}" --fail --silent --show-error -c "$agent_cookie_jar" \
  -H "Origin: ${gateway_base_url}" -H 'Content-Type: application/json' \
  -d "$agent_login_payload" "$gateway_base_url/api/v1/session")"
agent_csrf_token="$(jq -er '.csrfToken' <<<"$agent_login_response")"
agent_invitation="$(curl "${gateway_curl_args[@]}" --fail --silent --show-error -b "$agent_cookie_jar" \
  -H "Origin: ${gateway_base_url}" -H "X-CSRF-Token: $agent_csrf_token" -H 'Content-Type: application/json' \
  -d '{"name":"Kind E2E cluster"}' "$gateway_base_url/api/v1/admin/agent-installations")"
agent_installation_id="$(jq -er '.installationId' <<<"$agent_invitation")"
agent_enrollment_token="$(jq -er '.enrollmentToken' <<<"$agent_invitation")"
agent_token_patch="$(jq -cn --arg token "$(printf '%s' "$agent_enrollment_token" | base64)" '{data:{token:$token}}')"
kubectl --kubeconfig "$kubeconfig" -n fruto-system patch secret molejo-agent-enrollment --type merge -p "$agent_token_patch" >/dev/null
agent_enrollment_token=""; agent_invitation=""; agent_token_patch=""
wait_agent_state Paired
echo "cluster Agent paired"

identity_keys="$(kubectl --kubeconfig "$kubeconfig" -n fruto-system get secret molejo-agent-identity -o json | jq -r '.data | keys[]')"
for key in enrollment-attempt-id private-key.pem csr.pem installation-id tls.crt ca.crt certificate-not-after; do
  grep -Fxq "$key" <<<"$identity_keys"
done
[[ -z "$(kubectl --kubeconfig "$kubeconfig" -n fruto-system get secret molejo-agent-enrollment -o jsonpath='{.data.token}')" ]]
[[ "$(kubectl --kubeconfig "$kubeconfig" -n fruto-system get secret molejo-agent-identity -o jsonpath='{.data.installation-id}' | base64 --decode)" == "$agent_installation_id" ]]
agent_database_state="$(kubectl --kubeconfig "$kubeconfig" -n fruto-control-plane exec deployment/postgres -- \
  psql -U fruto -d fruto -Atc "SELECT status || ':' || (last_seen_at IS NOT NULL)::text FROM agent_installations WHERE public_id = '${agent_installation_id}'")"
[[ "$agent_database_state" == "Active:true" ]]
run_cluster_browser

echo "control-plane hierarchy E2E passed"
