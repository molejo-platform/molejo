#!/usr/bin/env bash
set -euo pipefail

for command in jq kubectl openssl; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR outside the checkout}"
case "$release_dir" in
  "$PWD"/*) echo "OpenBao recovery material must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_DIR must be absolute" >&2; exit 2;;
esac
actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

kubectl --context "$context" apply --server-side -k deploy/openbao-lab >/dev/null
kubectl --context "$context" -n molejo-secrets wait --for=condition=Ready certificate/openbao-server --timeout=300s
kubectl --context "$context" -n molejo-secrets rollout status statefulset/openbao --timeout=300s

umask 077
mkdir -p "$release_dir/secrets"
init_file="$release_dir/secrets/openbao-init.json"
fingerprint_file="$release_dir/secrets/parameter-fingerprint-key"
if [[ ! -s "$fingerprint_file" ]]; then
  openssl rand -base64 48 >"$fingerprint_file"
fi
status_json="$(kubectl --context "$context" -n molejo-secrets exec openbao-0 -- bao status -format=json 2>/dev/null || true)"
initialized="$(jq -r '.initialized' <<<"$status_json")"
if [[ "$initialized" == false ]]; then
  kubectl --context "$context" -n molejo-secrets exec openbao-0 -- bao operator init -key-shares=3 -key-threshold=2 -format=json >"$init_file"
fi
[[ -s "$init_file" ]] || { echo "OpenBao recovery material is unavailable at the approved release directory" >&2; exit 1; }
chmod 0600 "$init_file" "$fingerprint_file"

status_json="$(kubectl --context "$context" -n molejo-secrets exec openbao-0 -- bao status -format=json 2>/dev/null || true)"
sealed="$(jq -r '.sealed' <<<"$status_json")"
if [[ "$sealed" == true ]]; then
  for index in 0 1; do
    jq -r ".unseal_keys_b64[$index]" "$init_file" | kubectl --context "$context" -n molejo-secrets exec -i openbao-0 -- sh -c 'read -r key; bao operator unseal "$key" >/dev/null'
  done
fi

root_token="$(jq -r '.root_token' "$init_file")"
bao() {
  { printf '%s\n' "$root_token"; cat; } |
    kubectl --context "$context" -n molejo-secrets exec -i openbao-0 -- sh -c 'read -r BAO_TOKEN; export BAO_TOKEN; exec bao "$@"' sh "$@"
}
bao auth enable kubernetes >/dev/null 2>&1 || true
bao write auth/kubernetes/config \
  kubernetes_host=https://kubernetes.default.svc:443 \
  token_reviewer_jwt=@/var/run/secrets/kubernetes.io/serviceaccount/token \
  kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt >/dev/null
bao secrets enable -path=parameters -version=2 kv >/dev/null 2>&1 || true
printf '%s\n' 'path "parameters/data/workspaces/*" { capabilities = ["create", "update"] }' |
  bao policy write molejo-parameter-writer - >/dev/null
bao write auth/kubernetes/role/molejo-parameter-writer \
  bound_service_account_names=control-plane-api \
  bound_service_account_namespaces=fruto-control-plane \
  policies=molejo-parameter-writer ttl=15m >/dev/null

kubectl --context "$context" -n molejo-secrets get secret openbao-server-tls -o jsonpath='{.data.ca\.crt}' |
  base64 --decode >"$release_dir/secrets/openbao-ca.crt"
kubectl --context "$context" -n fruto-control-plane create configmap openbao-ca \
  --from-file=ca.crt="$release_dir/secrets/openbao-ca.crt" --dry-run=client -o yaml |
  kubectl --context "$context" apply -f - >/dev/null
kubectl --context "$context" -n fruto-control-plane create secret generic fruto-parameter-fingerprint \
  --from-file=key="$fingerprint_file" --dry-run=client -o yaml |
  kubectl --context "$context" apply -f - >/dev/null

printf 'OpenBao is initialized, unsealed, and configured; recovery material remains at %s\n' "$init_file"
