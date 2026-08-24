#!/usr/bin/env bash
set -euo pipefail

for command in kubectl yq; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
release_file="${FRUTO_RELEASE_OUTPUT:?set FRUTO_RELEASE_OUTPUT to the rendered release manifest}"
[[ -s "$release_file" ]] || { echo "release manifest does not exist" >&2; exit 2; }

actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }
for secret in fruto-control-plane-postgres fruto-control-plane-db fruto-control-plane-bootstrap registry-pull; do
  kubectl --context "$context" -n fruto-control-plane get secret "$secret" >/dev/null
done

kubectl --context "$context" apply --server-side --dry-run=server -f "$release_file" >/dev/null

yq ea 'select(.kind != "Deployment" and .kind != "Job" and .kind != "HTTPRoute")' "$release_file" |
  kubectl --context "$context" apply --server-side -f - >/dev/null
kubectl --context "$context" -n fruto-control-plane rollout status statefulset/fruto-control-plane-postgres --timeout=300s

kubectl --context "$context" -n fruto-control-plane delete job control-plane-migrate --ignore-not-found >/dev/null
yq ea 'select(.kind == "Job" and .metadata.name == "control-plane-migrate")' "$release_file" |
  kubectl --context "$context" apply --server-side -f - >/dev/null
kubectl --context "$context" -n fruto-control-plane wait --for=condition=complete job/control-plane-migrate --timeout=300s

kubectl --context "$context" -n fruto-control-plane delete job control-plane-bootstrap --ignore-not-found >/dev/null
yq ea 'select(.kind == "Job" and .metadata.name == "control-plane-bootstrap")' "$release_file" |
  kubectl --context "$context" apply --server-side -f - >/dev/null
kubectl --context "$context" -n fruto-control-plane wait --for=condition=complete job/control-plane-bootstrap --timeout=300s

yq ea 'select(.kind == "Deployment" or .kind == "HTTPRoute")' "$release_file" |
  kubectl --context "$context" apply --server-side -f - >/dev/null
kubectl --context "$context" -n fruto-control-plane rollout status deployment/control-plane-api --timeout=300s
kubectl --context "$context" -n fruto-control-plane rollout status deployment/console-web --timeout=300s

for route in control-plane-console control-plane-redirect; do
  for _ in $(seq 1 60); do
    accepted="$(kubectl --context "$context" -n fruto-control-plane get httproute "$route" -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}' 2>/dev/null || true)"
    resolved="$(kubectl --context "$context" -n fruto-control-plane get httproute "$route" -o jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}' 2>/dev/null || true)"
    [[ "$accepted" == True && "$resolved" == True ]] && break
    sleep 2
  done
  [[ "$accepted" == True && "$resolved" == True ]] || { echo "HTTPRoute $route was not accepted" >&2; exit 1; }
done

printf 'control plane release applied to context %s\n' "$context"
