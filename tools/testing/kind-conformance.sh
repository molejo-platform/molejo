#!/usr/bin/env bash
set -Eeuo pipefail

readonly kind_node_image="kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5"
readonly registry_image="docker.io/library/registry:3.0.0@sha256:6c5666b861f3505b116bb9aa9b25175e71210414bd010d92035ff64018f9457e"
readonly version="0.0.0-kind.1"

repository_root="$(git rev-parse --show-toplevel)"
run_id="$(date +%s)-$$"
cluster_name="molejo-conformance-${run_id}"
registry_name="molejo-conformance-registry-${run_id}"
context_name="kind-${cluster_name}"
work_directory="$(mktemp -d "${TMPDIR:-/tmp}/molejo-kind-conformance.XXXXXX")"
kubeconfig_file="${work_directory}/kubeconfig"
bundle_directory="${work_directory}/bundle"
molejoctl_bin="${work_directory}/molejoctl"
owner_password_file="${work_directory}/owner-password"
api_ca_file="${work_directory}/api-ca.crt"
result_file="${work_directory}/result.json"
port_forward_log="${work_directory}/port-forward.log"
port_forward_pid=""
registry_port=""
declare -a built_images=()
export KUBECONFIG="$kubeconfig_file"

kind() {
  GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" tool kind "$@"
}

progress() {
  printf '\n==> %s\n' "$1"
}

require_command() {
  command -v "$1" >/dev/null || {
    echo "required command not found: $1" >&2
    exit 1
  }
}

collect_diagnostics() {
  local diagnostics="${work_directory}/diagnostics"
  mkdir -p "$diagnostics"
  docker inspect "$registry_name" >"${diagnostics}/registry-inspect.json" 2>&1 || true
  docker logs "$registry_name" >"${diagnostics}/registry.log" 2>&1 || true
  kind export logs "${diagnostics}/kind" --name "$cluster_name" >/dev/null 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" get namespaces,pods,deployments,statefulsets -A -o wide >"${diagnostics}/workloads.txt" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" get events -A --sort-by=.lastTimestamp >"${diagnostics}/events.txt" 2>&1 || true
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM

  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi

  if [[ $status -ne 0 ]]; then
    collect_diagnostics
    rm -f -- "$owner_password_file" "$api_ca_file"
    echo "Conformance failed; diagnostics retained at ${work_directory}" >&2
  fi

  if [[ "${MOLEJO_KIND_KEEP:-0}" != "1" ]]; then
    kind delete cluster --name "$cluster_name" >/dev/null 2>&1 || true
    docker rm -f "$registry_name" >/dev/null 2>&1 || true
    if [[ ${#built_images[@]} -gt 0 ]]; then
      docker image rm "${built_images[@]}" >/dev/null 2>&1 || true
    fi
    if [[ $status -eq 0 ]]; then
      case "$work_directory" in
        "${TMPDIR:-/tmp}"/molejo-kind-conformance.*) rm -rf -- "$work_directory" ;;
      esac
    fi
  else
    echo "MOLEJO_KIND_KEEP=1; cluster ${cluster_name} and registry ${registry_name} were preserved" >&2
    echo "Kubeconfig: ${kubeconfig_file}" >&2
  fi

  exit "$status"
}

trap cleanup EXIT INT TERM

assert_can_i() {
  local expected="$1"
  local service_account="$2"
  local verb="$3"
  local resource="$4"
  local namespace="$5"
  local service_account_namespace="${service_account%%/*}"
  local service_account_name="${service_account#*/}"
  local actual
  if [[ "$service_account_namespace" == "$service_account" || -z "$service_account_name" ]]; then
    echo "invalid ServiceAccount identity: ${service_account}" >&2
    return 1
  fi
  actual="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" auth can-i \
    --as="system:serviceaccount:${service_account_namespace}:${service_account_name}" "$verb" "$resource" --namespace "$namespace" || true)"
  if [[ "$actual" != "$expected" ]]; then
    echo "RBAC assertion failed: ${service_account} can ${verb} ${resource} in ${namespace}: got ${actual}, want ${expected}" >&2
    return 1
  fi
}

wait_for_registry() {
  local attempt
  for attempt in $(seq 1 30); do
    if curl --fail --silent "http://127.0.0.1:${registry_port}/v2/" >/dev/null 2>&1; then
      return
    fi
    if [[ "$(docker inspect "$registry_name" --format '{{.State.Running}}' 2>/dev/null || true)" != "true" ]]; then
      docker logs "$registry_name" >&2 || true
      echo "local registry stopped before becoming ready" >&2
      return 1
    fi
    sleep 1
  done
  echo "local registry did not become ready" >&2
  return 1
}

build_and_push() {
  local name="$1"
  local dockerfile="$2"
  local architecture="$3"
  local commit="$4"
  local local_reference="molejo-conformance/${name}:${run_id}"
  local remote_reference="127.0.0.1:${registry_port}/molejo-platform/${name}:${run_id}"
  local archive="${work_directory}/${name}.tar"
  local digest

  docker buildx build \
    --platform "linux/${architecture}" \
    --build-arg "VERSION=v${version}" \
    --build-arg "COMMIT=${commit}" \
    --file "${repository_root}/${dockerfile}" \
    --tag "$local_reference" \
    --load \
    "$repository_root"
  wait_for_registry
  docker save --output "$archive" "$local_reference"
  GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" tool crane push --insecure "$archive" "$remote_reference"
  built_images+=("$local_reference")

  digest="$(GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" tool crane digest --insecure "$remote_reference")"
  if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "image ${name} has an invalid digest: ${digest}" >&2
    return 1
  fi
  built_canonical_reference="ghcr.io/molejo-platform/${name}@${digest}"
}

progress "Checking local dependencies"
for command_name in curl docker go helm jq kubectl; do
  require_command "$command_name"
done
docker info >/dev/null
kind version

progress "Starting the disposable registry"
docker run --detach --label molejo.conformance=true --name "$registry_name" --publish 127.0.0.1::5000 "$registry_image" >/dev/null
registry_port="$(docker port "$registry_name" 5000/tcp | sed -E 's/.*:([0-9]+)$/\1/' | head -n 1)"
[[ "$registry_port" =~ ^[0-9]+$ ]] || {
  echo "could not discover the local registry port" >&2
  exit 1
}
wait_for_registry

architecture="$(docker info --format '{{.Architecture}}')"
case "$architecture" in
  x86_64) architecture="amd64" ;;
  aarch64) architecture="arm64" ;;
  amd64 | arm64) ;;
  *)
    echo "unsupported Docker architecture: ${architecture}" >&2
    exit 1
    ;;
esac
commit="$(git -C "$repository_root" rev-parse HEAD)"

progress "Building and publishing immutable test images"
build_and_push platform-operator services/platform-operator/Dockerfile "$architecture" "$commit"
operator_image="$built_canonical_reference"
build_and_push cluster-agent services/cluster-agent/Dockerfile "$architecture" "$commit"
agent_image="$built_canonical_reference"
build_and_push control-plane-api services/control-plane-api/Dockerfile "$architecture" "$commit"
api_image="$built_canonical_reference"
build_and_push console-web apps/console-web/Dockerfile "$architecture" "$commit"
console_image="$built_canonical_reference"
build_and_push conformance-http test/fixtures/conformance-http/Dockerfile "$architecture" "$commit"
fixture_image="$built_canonical_reference"

progress "Packaging the release charts used by the product installer"
mkdir -p "$bundle_directory"
GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" run ./cmd/conformance-bundle \
  --version "$version" \
  --output "$bundle_directory" \
  --image "platform-operator=${operator_image}" \
  --image "cluster-agent=${agent_image}" \
  --image "control-plane-api=${api_image}" \
  --image "console-web=${console_image}"

GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "$repository_root" build \
  -ldflags="-X main.version=v${version} -X main.commit=${commit} -X main.buildDate=kind" \
  -o "$molejoctl_bin" ./apps/molejoctl

progress "Starting the disposable Kind cluster"
kind create cluster --name "$cluster_name" --image "$kind_node_image" --kubeconfig "$kubeconfig_file" --wait 180s
docker network connect kind "$registry_name"

while IFS= read -r node_name; do
  docker exec "$node_name" curl --fail --silent --show-error "http://${registry_name}:5000/v2/" >/dev/null
  docker exec "$node_name" mkdir -p /etc/containerd/certs.d/ghcr.io
  printf 'server = "http://%s:5000"\n\n[host."http://%s:5000"]\n  capabilities = ["pull", "resolve"]\n' "$registry_name" "$registry_name" |
    docker exec -i "$node_name" sh -c 'cat > /etc/containerd/certs.d/ghcr.io/hosts.toml'
done < <(kind get nodes --name "$cluster_name")

kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" create configmap local-registry-hosting \
  --namespace kube-public \
  --from-literal="localRegistryHosting.v1=host: \"localhost:${registry_port}\"\nhelp: \"https://kind.sigs.k8s.io/docs/user/local-registry/\"" \
  --dry-run=client -o yaml |
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" apply -f - >/dev/null

progress "Installing the runtime and control plane twice"
for attempt in 1 2; do
  "$molejoctl_bin" platform runtime install \
    --kube-context "$context_name" \
    --version "$version" \
    --chart-path "${bundle_directory}/molejo-cluster-${version}.tgz"
done
for attempt in 1 2; do
  "$molejoctl_bin" platform control-plane install \
    --kube-context "$context_name" \
    --version "$version" \
    --chart-path "${bundle_directory}/molejo-control-plane-${version}.tgz"
done
"$molejoctl_bin" platform doctor --kube-context "$context_name"

progress "Verifying PostgreSQL storage and the Agent mTLS session"
kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-control-plane wait \
  --for=jsonpath='{.status.phase}'=Bound pvc/data-postgres-0 --timeout=120s
kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-system wait \
  --for=condition=Available deployment/cluster-agent --timeout=120s

kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-control-plane get secret molejo-control-plane-bootstrap \
  -o jsonpath='{.data.owner-password}' | base64 --decode >"$owner_password_file"
kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-control-plane get secret molejo-control-plane-server-ca \
  -o jsonpath='{.data.ca\.crt}' | base64 --decode >"$api_ca_file"
chmod 600 "$owner_password_file" "$api_ca_file"

kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-control-plane port-forward service/control-plane-api :8444 >"$port_forward_log" 2>&1 &
port_forward_pid=$!
for _ in $(seq 1 60); do
  api_port="$(sed -n -E 's/.*127\.0\.0\.1:([0-9]+) -> 8444.*/\1/p' "$port_forward_log" | head -n 1)"
  [[ "$api_port" =~ ^[0-9]+$ ]] && break
  kill -0 "$port_forward_pid" >/dev/null 2>&1 || {
    cat "$port_forward_log" >&2
    exit 1
  }
  sleep 1
done
[[ "${api_port:-}" =~ ^[0-9]+$ ]] || {
  echo "control plane API port-forward did not become ready" >&2
  exit 1
}

progress "Running the application lifecycle and observability journey"
GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" run ./cmd/kubernetes-conformance \
  --endpoint "https://127.0.0.1:${api_port}" \
  --ca-file "$api_ca_file" \
  --password-file "$owner_password_file" \
  --image "$fixture_image" \
  --result-file "$result_file"

workspace_namespace="$(jq -r '.namespace' "$result_file")"
[[ "$workspace_namespace" =~ ^ws-[a-z0-9]+$ ]] || {
  echo "journey returned an invalid workspace namespace: ${workspace_namespace}" >&2
  exit 1
}

progress "Checking positive and negative namespace RBAC boundaries"
assert_can_i yes molejo-system/cluster-agent create secrets "$workspace_namespace"
assert_can_i no molejo-system/cluster-agent list secrets "$workspace_namespace"
assert_can_i yes molejo-system/cluster-agent get pods/log "$workspace_namespace"
assert_can_i no molejo-system/cluster-agent get secrets molejo-control-plane
assert_can_i no molejo-system/cluster-agent get pods default
assert_can_i yes molejo-system/platform-operator create deployments.apps "$workspace_namespace"
assert_can_i no molejo-system/platform-operator create deployments.apps default
assert_can_i no molejo-system/workspace-boundary-controller create deployments.apps "$workspace_namespace"
assert_can_i no molejo-system/workspace-boundary-controller create secrets "$workspace_namespace"

progress "Checking idempotent application cleanup"
if kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$workspace_namespace" get appdeployments --no-headers 2>/dev/null | grep -q .; then
  echo "AppDeployment resources remained after deletion" >&2
  exit 1
fi
if kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$workspace_namespace" get deployments,services \
  --selector app.kubernetes.io/managed-by=molejo-platform-operator --no-headers 2>/dev/null | grep -q .; then
  echo "managed workloads remained after deletion" >&2
  exit 1
fi

progress "Deleting the cluster and checking host-side residue"
kind delete cluster --name "$cluster_name"
docker rm -f "$registry_name" >/dev/null
for image_reference in "${built_images[@]}"; do
  docker image rm "$image_reference" >/dev/null 2>&1 || true
done
built_images=()

if docker ps --all --filter "name=^/${cluster_name}-" --format '{{.Names}}' | grep -q .; then
  echo "Kind containers remained after teardown" >&2
  exit 1
fi
if docker ps --all --filter "name=^/${registry_name}$" --format '{{.Names}}' | grep -q .; then
  echo "registry container remained after teardown" >&2
  exit 1
fi

echo
echo "Result: Kind conformance healthy"
