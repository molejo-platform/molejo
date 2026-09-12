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
if [[ -n "${MOLEJO_CONFORMANCE_OUTPUT_DIR:-}" ]]; then
  work_directory="${MOLEJO_CONFORMANCE_OUTPUT_DIR%/}/${run_id}"
  mkdir -p "$work_directory"
  chmod 700 "$work_directory"
else
  work_directory="$(mktemp -d "${TMPDIR:-/tmp}/molejo-kind-conformance.XXXXXX")"
fi
scratch_directory="$(mktemp -d "${TMPDIR:-/tmp}/molejo-kind-conformance-scratch.XXXXXX")"
chmod 700 "$scratch_directory"
kubeconfig_file="${scratch_directory}/kubeconfig"
bundle_directory="${scratch_directory}/bundle"
molejoctl_bin="${scratch_directory}/molejoctl"
conformance_bin="${scratch_directory}/molejo-conformance"
owner_password_file="${scratch_directory}/owner-password"
api_ca_file="${scratch_directory}/api-ca.crt"
result_directory="${work_directory}/results"
result_file="${result_directory}/report.json"
publication_result_directory="${work_directory}/publication-results"
publication_result_file="${publication_result_directory}/report.json"
publication_ca_file="${scratch_directory}/publication-ca.crt"
publication_key_file="${scratch_directory}/publication-tls.key"
port_forward_log="${work_directory}/port-forward.log"
port_forward_pid=""
gateway_port_forward_log="${work_directory}/gateway-port-forward.log"
gateway_port_forward_pid=""
registry_port=""
declare -a built_images=()
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
current_phase="initialization"
source_revision="unknown"
export KUBECONFIG="$kubeconfig_file"

kind() {
  GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" tool kind "$@"
}

progress() {
  current_phase="$1"
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
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" get workspaceplacements.platform.molejo.dev -o yaml >"${diagnostics}/workspace-placements.yaml" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" get rolebindings.rbac.authorization.k8s.io -A -o yaml >"${diagnostics}/role-bindings.yaml" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-control-plane logs deployment/control-plane-api --tail=500 >"${diagnostics}/control-plane-api.log" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-control-plane logs deployment/control-plane-api --previous --tail=500 >"${diagnostics}/control-plane-api-previous.log" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-system logs deployment/cluster-agent --tail=500 >"${diagnostics}/cluster-agent.log" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-system logs deployment/cluster-agent --previous --tail=500 >"${diagnostics}/cluster-agent-previous.log" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-system logs deployment/workspace-boundary-controller --tail=500 >"${diagnostics}/workspace-boundary-controller.log" 2>&1 || true
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-system logs deployment/workspace-boundary-controller --previous --tail=500 >"${diagnostics}/workspace-boundary-controller-previous.log" 2>&1 || true
}

write_harness_report() {
  local status="$1" reason="$2" exit_code="$3" finished_at
  local core_available=false publication_available=false xml_reason
  finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  [[ -f "$result_file" ]] && core_available=true
  [[ -f "$publication_result_file" ]] && publication_available=true
  jq -n \
    --arg schemaVersion "molejo-kind-conformance-harness.v1alpha1" \
    --arg status "$status" \
    --arg reason "$reason" \
    --arg revision "$source_revision" \
    --arg failedPhase "$current_phase" \
    --arg startedAt "$started_at" \
    --arg finishedAt "$finished_at" \
    --argjson exitCode "$exit_code" \
    --argjson coreAvailable "$core_available" \
    --argjson publicationAvailable "$publication_available" \
    '{schemaVersion:$schemaVersion,status:$status,reason:$reason,revision:$revision,failedPhase:(if $status == "PASS" then null else $failedPhase end),startedAt:$startedAt,finishedAt:$finishedAt,exitCode:$exitCode,profiles:[{id:"alpha-core/v1",report:"results/report.json",available:$coreAvailable},{id:"http-publication/v1",report:"publication-results/report.json",available:$publicationAvailable}]}' \
    >"${work_directory}/harness-report.json.tmp" || return 1
  xml_reason="$(jq -nr --arg value "$reason" '$value|@html')" || return 1
  if [[ "$status" == "PASS" ]]; then
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' '<testsuites name="molejo-kind-conformance" tests="1" failures="0"><testsuite name="harness" tests="1" failures="0"><testcase name="kind-conformance" classname="molejo-kind-conformance"/></testsuite></testsuites>' >"${work_directory}/harness-junit.xml.tmp" || return 1
  else
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' "<testsuites name=\"molejo-kind-conformance\" tests=\"1\" failures=\"1\"><testsuite name=\"harness\" tests=\"1\" failures=\"1\"><testcase name=\"kind-conformance\" classname=\"molejo-kind-conformance\"><failure type=\"FAIL\" message=\"${xml_reason}\"/></testcase></testsuite></testsuites>" >"${work_directory}/harness-junit.xml.tmp" || return 1
  fi
  chmod 600 "${work_directory}/harness-report.json.tmp" "${work_directory}/harness-junit.xml.tmp" || return 1
  # JSON is the authoritative verdict and is published last. A consumer can
  # therefore never observe PASS before its derived JUnit evidence is durable.
  mv "${work_directory}/harness-junit.xml.tmp" "${work_directory}/harness-junit.xml" || return 1
  if ! mv "${work_directory}/harness-report.json.tmp" "${work_directory}/harness-report.json"; then
    rm -f -- "${work_directory}/harness-junit.xml"
    return 1
  fi
}

cleanup() {
  local original_status=$? final_status="PASS" reason="" teardown_failed=false image_reference
  trap - EXIT INT TERM
  set +e

  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    wait "$port_forward_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$gateway_port_forward_pid" ]]; then
    kill "$gateway_port_forward_pid" >/dev/null 2>&1 || true
    wait "$gateway_port_forward_pid" >/dev/null 2>&1 || true
  fi

  if [[ $original_status -ne 0 ]]; then
    collect_diagnostics
    echo "Conformance failed; diagnostics retained at ${work_directory}" >&2
    final_status="FAIL"
    reason="failed during phase: ${current_phase}"
  fi

  kind delete cluster --name "$cluster_name" >/dev/null 2>&1 || teardown_failed=true
  if docker inspect "$registry_name" >/dev/null 2>&1; then
    docker rm -f "$registry_name" >/dev/null 2>&1 || teardown_failed=true
  fi
  if [[ ${#built_images[@]} -gt 0 ]]; then
    docker image rm "${built_images[@]}" >/dev/null 2>&1 || teardown_failed=true
    for image_reference in "${built_images[@]}"; do
      docker image inspect "$image_reference" >/dev/null 2>&1 && teardown_failed=true
    done
  fi
  if docker ps --all --filter "name=^/${cluster_name}-" --format '{{.Names}}' | grep -q .; then
    teardown_failed=true
  fi
  if docker ps --all --filter "name=^/${registry_name}$" --format '{{.Names}}' | grep -q .; then
    teardown_failed=true
  fi
  rm -rf -- "$scratch_directory"

  if [[ "$teardown_failed" == true ]]; then
    final_status="FAIL"
    reason="${reason:+${reason}; }disposable infrastructure teardown was incomplete"
    original_status=1
  fi
  if ! write_harness_report "$final_status" "$reason" "$original_status"; then
    echo "Conformance evidence could not be published" >&2
    original_status=2
    final_status="FAIL"
  fi

  echo "Conformance evidence: ${work_directory}" >&2
  if [[ "$final_status" == "PASS" ]]; then
    echo "Result: Molejo Kind conformance passed"
  fi

    exit "$original_status"
}

trap cleanup EXIT
trap 'exit 130' INT TERM

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

assert_withdrawn_tombstone() {
  local namespace="$1"
  local count withdrawn condition reason tombstone_uid
  count="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$namespace" get appdeployments --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  [[ "$count" == "1" ]] || {
    echo "expected one terminal AppDeployment tombstone in ${namespace}, found ${count}" >&2
    return 1
  }
  withdrawn="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$namespace" get appdeployments -o jsonpath='{.items[0].spec.withdrawn}')"
  condition="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$namespace" get appdeployments -o jsonpath='{.items[0].status.conditions[?(@.type=="Withdrawn")].status}')"
  reason="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$namespace" get appdeployments -o jsonpath='{.items[0].status.conditions[?(@.type=="Withdrawn")].reason}')"
  tombstone_uid="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$namespace" get appdeployments -o jsonpath='{.items[0].metadata.uid}')"
  [[ "$withdrawn" == "true" && "$condition" == "True" && "$reason" == "ChildrenRemoved" ]] || {
    echo "terminal AppDeployment in ${namespace} does not carry the confirmed withdrawal fence" >&2
    return 1
  }
  if kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$namespace" get deployments,services,configmaps,secrets,httproutes.gateway.networking.k8s.io \
    --selector app.kubernetes.io/managed-by=molejo-platform-operator --no-headers 2>/dev/null | grep -q .; then
    echo "managed application children remained after withdrawal in ${namespace}" >&2
    return 1
  fi
  if kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace "$namespace" get deployments,services,configmaps,secrets,httproutes.gateway.networking.k8s.io -o json |
    jq -e --arg uid "$tombstone_uid" 'any(.items[]; any(.metadata.ownerReferences[]?; .uid == $uid))' >/dev/null; then
    echo "application children owned by the terminal AppDeployment remained in ${namespace}" >&2
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
  local archive="${scratch_directory}/${name}.tar"
  local digest

  docker buildx build \
    --platform "linux/${architecture}" \
    --build-arg "VERSION=v${version}" \
    --build-arg "COMMIT=${commit}" \
    --file "${repository_root}/${dockerfile}" \
    --tag "$local_reference" \
    --load \
    "$repository_root"
  built_images+=("$local_reference")
  wait_for_registry
  docker save --output "$archive" "$local_reference"
  GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" tool crane push --insecure "$archive" "$remote_reference"

  digest="$(GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" tool crane digest --insecure "$remote_reference")"
  if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "image ${name} has an invalid digest: ${digest}" >&2
    return 1
  fi
  built_canonical_reference="ghcr.io/molejo-platform/${name}@${digest}"
}

progress "Checking local dependencies"
for command_name in curl docker go helm jq kubectl openssl; do
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
source_revision="$commit"
if [[ -n "$(git -C "$repository_root" status --porcelain)" ]]; then
  source_revision="${commit}-dirty"
fi

progress "Building and publishing immutable test images"
build_and_push platform-operator services/platform-operator/Dockerfile "$architecture" "$source_revision"
operator_image="$built_canonical_reference"
build_and_push cluster-agent services/cluster-agent/Dockerfile "$architecture" "$source_revision"
agent_image="$built_canonical_reference"
build_and_push control-plane-api services/control-plane-api/Dockerfile "$architecture" "$source_revision"
api_image="$built_canonical_reference"
build_and_push console-web apps/console-web/Dockerfile "$architecture" "$source_revision"
console_image="$built_canonical_reference"
build_and_push conformance-http test/fixtures/conformance-http/Dockerfile "$architecture" "$source_revision"
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
  -ldflags="-X main.version=v${version} -X main.commit=${source_revision} -X main.buildDate=kind" \
  -o "$molejoctl_bin" ./apps/molejoctl
GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C "${repository_root}/tools" build \
  -ldflags="-X main.version=v${version} -X main.commit=${source_revision}" \
  -o "$conformance_bin" ./cmd/molejo-conformance

progress "Starting the disposable Kind cluster"
kind create cluster --name "$cluster_name" --image "$kind_node_image" --kubeconfig "$kubeconfig_file" --wait 180s
docker network connect kind "$registry_name"

progress "Installing the local Gateway API and TLS edge"
gateway_module="$(go list -m -f '{{.Dir}}' sigs.k8s.io/gateway-api)"
for crd in "${gateway_module}"/config/crd/experimental/gateway.networking.k8s.io_*.yaml; do
  kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" apply --server-side -f "$crd" >/dev/null
done
helm install publication-edge oci://ghcr.io/traefik/helm/traefik \
  --version 41.2.0 \
  --namespace publication-edge \
  --create-namespace \
  --set fullnameOverride=publication-edge \
  --set providers.kubernetesGateway.enabled=true \
  --set providers.kubernetesIngress.enabled=false \
  --set gateway.enabled=false \
  --set gatewayClass.name=publication-edge \
  --set service.type=ClusterIP \
  --wait --timeout 180s >"${work_directory}/gateway-helm.log"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj '/CN=example.test' \
  -addext 'subjectAltName=DNS:example.test,DNS:*.apps.example.test' \
  -keyout "$publication_key_file" \
  -out "$publication_ca_file" >/dev/null 2>&1
chmod 600 "$publication_ca_file" "$publication_key_file"
kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace publication-edge create secret tls publication-tls \
  --cert "$publication_ca_file" --key "$publication_key_file" >/dev/null
cat <<'YAML' | kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" apply -f - >/dev/null
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: external
  namespace: publication-edge
spec:
  gatewayClassName: publication-edge
  listeners:
    - name: apex
      hostname: example.test
      port: 8443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs: [{name: publication-tls}]
      allowedRoutes:
        namespaces:
          from: Selector
          selector:
            matchLabels: {platform.molejo.dev/http-publication: enabled}
    - name: pool
      hostname: '*.apps.example.test'
      port: 8443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs: [{name: publication-tls}]
      allowedRoutes:
        namespaces:
          from: Selector
          selector:
            matchLabels: {platform.molejo.dev/http-publication: enabled}
YAML
kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace publication-edge port-forward service/publication-edge :443 >"$gateway_port_forward_log" 2>&1 &
gateway_port_forward_pid=$!
for _ in $(seq 1 60); do
  gateway_port="$(sed -n -E 's/.*127\.0\.0\.1:([0-9]+) -> [0-9]+.*/\1/p' "$gateway_port_forward_log" | head -n 1)"
  [[ "$gateway_port" =~ ^[0-9]+$ ]] && break
  kill -0 "$gateway_port_forward_pid" >/dev/null 2>&1 || {
    cat "$gateway_port_forward_log" >&2
    exit 1
  }
  sleep 1
done
[[ "${gateway_port:-}" =~ ^[0-9]+$ ]] || {
  echo "publication Gateway port-forward did not become ready" >&2
  exit 1
}

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
cluster_id="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace molejo-control-plane get secret molejo-control-plane-bootstrap -o jsonpath='{.data.agent-installation-id}' | base64 --decode)"
cluster_uid="$(kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$cluster_id" =~ ^(cls|agi)-[a-z2-7]{20}$ ]] || {
  echo "bootstrap returned an invalid Molejo cluster ID" >&2
  exit 1
}
[[ -n "$cluster_uid" ]] || {
  echo "Kubernetes cluster UID is empty" >&2
  exit 1
}

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
"$conformance_bin" plan --profile alpha-core --cluster-id "$cluster_id" --cluster-uid "$cluster_uid" --kube-context "$context_name" --disposable-target
"$conformance_bin" run \
  --profile alpha-core \
  --endpoint "https://127.0.0.1:${api_port}" \
  --ca-file "$api_ca_file" \
  --password-file "$owner_password_file" \
  --image "$fixture_image" \
  --output "$result_directory" \
  --cluster-id "$cluster_id" \
  --cluster-uid "$cluster_uid" \
  --kube-context "$context_name" \
  --disposable-target

workspace_namespace="$(jq -r '.outputs.namespace' "$result_file")"
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
assert_can_i no molejo-system/platform-operator get secrets publication-edge
assert_can_i no molejo-system/platform-operator update gateways.gateway.networking.k8s.io publication-edge
assert_can_i no molejo-system/workspace-boundary-controller create deployments.apps "$workspace_namespace"
assert_can_i no molejo-system/workspace-boundary-controller create secrets "$workspace_namespace"
denied_name="conformance-denied-${run_id}"
if kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --as=system:serviceaccount:molejo-system:platform-operator \
  --namespace default create configmap "$denied_name" --from-literal=probe=true >/dev/null 2>&1; then
  echo "platform-operator created a forbidden cross-namespace canary" >&2
  exit 1
fi
if kubectl --kubeconfig "$kubeconfig_file" --context "$context_name" --namespace default get configmap "$denied_name" >/dev/null 2>&1; then
  echo "forbidden RBAC canary exists after the denied request" >&2
  exit 1
fi

progress "Checking idempotent application cleanup"
assert_withdrawn_tombstone "$workspace_namespace"

progress "Running exact and pooled HTTP publication with trusted TLS"
"$conformance_bin" plan --profile http-publication \
  --cluster-id "$cluster_id" \
  --cluster-uid "$cluster_uid" \
  --kube-context "$context_name" \
  --disposable-target \
  --publication-manage-binding \
  --publication-gateway-namespace publication-edge \
  --publication-gateway-name external \
  --publication-exact-host example.test \
  --publication-pool-domain apps.example.test \
  --publication-pool-label conformance \
  --publication-exact-listener apex \
  --publication-pool-listener pool \
  --publication-probe-address "127.0.0.1:${gateway_port}" \
  --publication-ca-file "$publication_ca_file"
"$conformance_bin" run \
  --profile http-publication \
  --endpoint "https://127.0.0.1:${api_port}" \
  --ca-file "$api_ca_file" \
  --password-file "$owner_password_file" \
  --image "$fixture_image" \
  --output "$publication_result_directory" \
  --cluster-id "$cluster_id" \
  --cluster-uid "$cluster_uid" \
  --kube-context "$context_name" \
  --disposable-target \
  --publication-manage-binding \
  --publication-gateway-namespace publication-edge \
  --publication-gateway-name external \
  --publication-exact-host example.test \
  --publication-pool-domain apps.example.test \
  --publication-pool-label conformance \
  --publication-exact-listener apex \
  --publication-pool-listener pool \
  --publication-probe-address "127.0.0.1:${gateway_port}" \
  --publication-ca-file "$publication_ca_file"
publication_workspace_namespace="$(jq -r '.outputs.namespace' "$publication_result_file")"
[[ "$publication_workspace_namespace" =~ ^ws-[a-z0-9]+$ ]] || {
  echo "publication journey returned an invalid workspace namespace: ${publication_workspace_namespace}" >&2
  exit 1
}
assert_withdrawn_tombstone "$publication_workspace_namespace"

progress "Finalizing disposable infrastructure and evidence"
