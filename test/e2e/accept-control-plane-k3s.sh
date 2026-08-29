#!/usr/bin/env bash
set -euo pipefail

for command in curl jq kubectl; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
api_image="${FRUTO_API_IMAGE:?set FRUTO_API_IMAGE}"
console_image="${FRUTO_CONSOLE_IMAGE:?set FRUTO_CONSOLE_IMAGE}"

actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

for job in control-plane-migrate control-plane-bootstrap; do
  [[ "$(kubectl --context "$context" -n fruto-control-plane get job "$job" -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}')" == True ]]
done
[[ "$(kubectl --context "$context" -n fruto-control-plane get deployment control-plane-api -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$api_image" ]]
[[ "$(kubectl --context "$context" -n fruto-control-plane get deployment control-plane-runtime-worker -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$api_image" ]]
[[ "$(kubectl --context "$context" -n fruto-control-plane get deployment control-plane-parameter-worker -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$api_image" ]]
[[ "$(kubectl --context "$context" -n fruto-control-plane get deployment console-web -o jsonpath='{.spec.template.spec.containers[0].image}')" == "$console_image" ]]

for route in control-plane-console; do
  conditions="$(kubectl --context "$context" -n fruto-control-plane get httproute "$route" -o json)"
  jq -e 'any(.status.parents[].conditions[]; .type == "Accepted" and .status == "True")' <<<"$conditions" >/dev/null
  jq -e 'any(.status.parents[].conditions[]; .type == "ResolvedRefs" and .status == "True")' <<<"$conditions" >/dev/null
done

api_identity="system:serviceaccount:fruto-control-plane:control-plane-api"
worker_identity="system:serviceaccount:fruto-control-plane:control-plane-runtime-worker"
parameter_worker_identity="system:serviceaccount:fruto-control-plane:control-plane-parameter-worker"
[[ "$(kubectl --context "$context" auth can-i get appdeployments.platform.fruto.calouro.tech --as="$api_identity" -n fruto-workspaces)" == no ]]
[[ "$(kubectl --context "$context" auth can-i get secrets --as="$api_identity" -n fruto-workspaces)" == no ]]
[[ "$(kubectl --context "$context" auth can-i get appdeployments.platform.fruto.calouro.tech --as="$worker_identity" -n fruto-workspaces)" == yes ]]
[[ "$(kubectl --context "$context" auth can-i patch appdeployments.platform.fruto.calouro.tech --as="$worker_identity" -n fruto-workspaces)" == yes ]]
[[ "$(kubectl --context "$context" auth can-i update appdeployments.platform.fruto.calouro.tech/status --as="$worker_identity" -n fruto-workspaces)" == no ]]
[[ "$(kubectl --context "$context" auth can-i create secrets --as="$worker_identity" -n fruto-workspaces)" == yes ]]
[[ "$(kubectl --context "$context" auth can-i list configmaps --as="$worker_identity" -n fruto-workspaces)" == yes ]]
[[ "$(kubectl --context "$context" auth can-i delete configmaps --as="$worker_identity" -n fruto-workspaces)" == yes ]]
[[ "$(kubectl --context "$context" auth can-i list secrets --as="$worker_identity" -n fruto-workspaces)" == yes ]]
[[ "$(kubectl --context "$context" auth can-i delete secrets --as="$worker_identity" -n fruto-workspaces)" == yes ]]
[[ "$(kubectl --context "$context" auth can-i get secrets --as="$worker_identity" -n fruto-control-plane)" == no ]]
[[ "$(kubectl --context "$context" auth can-i get deployments.apps --as="$worker_identity" -n fruto-workspaces)" == no ]]
[[ "$(kubectl --context "$context" auth can-i get secrets --as="$parameter_worker_identity" -n fruto-workspaces)" == no ]]

https_result="$(curl --silent --show-error --output /dev/null --write-out '%{http_code} %{ssl_verify_result}' --max-time 15 https://cloud.molejo.dev/)"
[[ "$https_result" == "200 0" ]]
capabilities="$(curl --silent --show-error --fail --max-time 15 https://cloud.molejo.dev/api/v1/auth/capabilities)"
jq -e '.password == true and .totp == true and .passkey == false' <<<"$capabilities" >/dev/null
http_headers="$(curl --silent --show-error --head --max-time 15 http://cloud.molejo.dev/)"
grep -Eq '^HTTP/[^ ]+ (301|308)' <<<"$http_headers"
grep -Eiq '^location: https://cloud\.molejo\.dev/?' <<<"$http_headers"

printf 'k3s acceptance passed: images=digest routes=Accepted/ResolvedRefs rbac=least-privilege tls=trusted redirect=https\n'
