#!/usr/bin/env bash
# Disposable local proof; never uses the caller's Kubernetes context.
set -Eeuo pipefail
repository_root="$(git rev-parse --show-toplevel)"
work_directory="$(mktemp -d "${TMPDIR:-/tmp}/molejo-publication.XXXXXX")"
run_id="$(date +%s)-$$"
cluster_name="molejo-publication-${run_id}"
fixture_tag="molejo-publication-fixture:${run_id}"
export KUBECONFIG="${work_directory}/kubeconfig"
export GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}"
port_forward_pid=""
kind() { go -C "${repository_root}/tools" tool kind "$@"; }
cleanup() {
  local result=$?
  trap - EXIT
  if [[ -n "$port_forward_pid" ]]; then kill "$port_forward_pid" 2>/dev/null || true; fi
  if [[ $result -ne 0 ]]; then
    kubectl get pods,appdeployments,httproutes,gateways -A -o wide >"${work_directory}/diagnostics.txt" 2>&1 || true
    echo "Local publication evidence: ${work_directory}" >&2
  fi
  kind delete cluster --name "$cluster_name" >/dev/null 2>&1 || true
  docker image rm "$fixture_tag" >/dev/null 2>&1 || true
  rm -f "$KUBECONFIG"
  exit "$result"
}
trap cleanup EXIT
printf 'Creating disposable local Gateway/TLS environment\n'
kind create cluster --name "$cluster_name" --image kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5 --kubeconfig "$KUBECONFIG" --wait 180s
architecture="$(docker info --format '{{.Architecture}}')"
case "$architecture" in aarch64) architecture=arm64;; x86_64) architecture=amd64;; esac
GOOS=linux GOARCH="$architecture" CGO_ENABLED=0 go build -o "${work_directory}/server" ./test/fixtures/conformance-http
cat >"${work_directory}/Dockerfile" <<'DOCKER'
FROM scratch
COPY server /server
USER 65532:65532
ENTRYPOINT ["/server"]
DOCKER
docker build --platform "linux/${architecture}" -t "molejo-publication-fixture:${run_id}" "$work_directory" >/dev/null
kind load docker-image "$fixture_tag" --name "$cluster_name"
node_name="$(kind get nodes --name "$cluster_name" | head -n 1)"
digest="$(docker exec "$node_name" ctr -n k8s.io images list | awk -v reference="docker.io/library/${fixture_tag}" '$1 == reference {print $3}')"
[[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]]
docker exec "$node_name" ctr -n k8s.io images tag "docker.io/library/${fixture_tag}" "publication.test/fixture@${digest}" >/dev/null
gateway_module="$(go list -m -f '{{.Dir}}' sigs.k8s.io/gateway-api)"
for crd in "${gateway_module}"/config/crd/experimental/gateway.networking.k8s.io_*.yaml; do
  kubectl apply --server-side -f "$crd" >/dev/null
done
kubectl apply --server-side -k deploy/crds >/dev/null
kubectl apply -f deploy/operator/rbac/role.yaml >/dev/null
helm install publication-edge oci://ghcr.io/traefik/helm/traefik --version 41.2.0 --namespace publication-edge --create-namespace --set fullnameOverride=publication-edge --set providers.kubernetesGateway.enabled=true --set providers.kubernetesIngress.enabled=false --set gateway.enabled=false --set gatewayClass.name=publication-edge --set service.type=ClusterIP --wait --timeout 180s >"${work_directory}/helm.log"
kubectl -n publication-edge port-forward service/publication-edge :443 >"${work_directory}/port-forward.log" 2>&1 &
port_forward_pid=$!
for ((attempt=0; attempt<100; attempt++)); do
  if rg -q 'Forwarding from 127.0.0.1:' "${work_directory}/port-forward.log"; then break; fi
  sleep 0.1
done
local_port="$(sed -nE 's/Forwarding from 127.0.0.1:([0-9]+) -> .*/\1/p' "${work_directory}/port-forward.log" | head -n 1)"
[[ "$local_port" =~ ^[0-9]+$ ]]
MOLEJO_PUBLICATION_KUBECONFIG="$KUBECONFIG" MOLEJO_PUBLICATION_IMAGE="publication.test/fixture@${digest}" MOLEJO_PUBLICATION_PORT="$local_port" go test ./services/platform-operator/internal/controller -run '^TestLocalGatewayTLSConformance$' -count=1 -v
printf 'PASS: local Host/SNI, certificate, shared workload, partial removal and RBAC\n'
