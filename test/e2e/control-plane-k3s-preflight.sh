#!/usr/bin/env bash
set -euo pipefail

for command in kubectl jq curl dig docker; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "$command is required for the k3s preflight" >&2
    exit 2
  }
done

target_context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
expected_server="${FRUTO_EXPECTED_KUBE_SERVER:?set FRUTO_EXPECTED_KUBE_SERVER}"
expected_cluster_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
certificate_namespace="${FRUTO_CERTIFICATE_NAMESPACE:-traefik-system}"
certificate_name="${FRUTO_CERTIFICATE_NAME:-molejo-public-tls}"
certificate_secret="${FRUTO_CERTIFICATE_SECRET:-molejo-public-tls}"

for variable in FRUTO_API_IMAGE FRUTO_CONSOLE_IMAGE FRUTO_TESTKIT_IMAGE; do
  reference="${!variable:-}"
  [[ "$reference" =~ ^[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}$ ]] || {
    echo "$variable must be an immutable image reference" >&2
    exit 2
  }
done

kubeconfig_json="$(kubectl config view -o json)"
cluster_name="$(jq -er --arg context "$target_context" '.contexts[] | select(.name == $context) | .context.cluster' <<<"$kubeconfig_json")"
actual_server="$(jq -er --arg cluster "$cluster_name" '.clusters[] | select(.name == $cluster) | .cluster.server' <<<"$kubeconfig_json")"
[[ "${actual_server%/}" == "${expected_server%/}" ]] || {
  echo "context $target_context resolves to unexpected server $actual_server" >&2
  exit 1
}

actual_cluster_uid="$(kubectl --context "$target_context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_cluster_uid" == "$expected_cluster_uid" ]] || {
  echo "cluster UID does not match the approved target" >&2
  exit 1
}

not_ready="$(kubectl --context "$target_context" get nodes -o json | jq '[.items[] | select(any(.status.conditions[]; .type == "Ready" and .status != "True"))] | length')"
[[ "$not_ready" == 0 ]] || { echo "$not_ready node(s) are not Ready" >&2; exit 1; }
non_amd64="$(kubectl --context "$target_context" get nodes -o json | jq '[.items[] | select(.status.nodeInfo.architecture != "amd64")] | length')"
[[ "$non_amd64" == 0 ]] || { echo "$non_amd64 node(s) are not linux/amd64 targets" >&2; exit 1; }

gateway_programmed="$(kubectl --context "$target_context" -n fruto-system get gateway fruto -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}')"
[[ "$gateway_programmed" == "True" ]] || { echo "Gateway fruto-system/fruto is not Programmed" >&2; exit 1; }
certificate_ready="$(kubectl --context "$target_context" -n "$certificate_namespace" get certificate "$certificate_name" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')"
[[ "$certificate_ready" == "True" ]] || { echo "wildcard certificate is not Ready" >&2; exit 1; }
actual_certificate_secret="$(kubectl --context "$target_context" -n "$certificate_namespace" get certificate "$certificate_name" -o jsonpath='{.spec.secretName}')"
[[ "$actual_certificate_secret" == "$certificate_secret" ]] || {
  echo "certificate $certificate_namespace/$certificate_name uses unexpected Secret $actual_certificate_secret" >&2
  exit 1
}

[[ -n "$(dig +short cloud.molejo.dev A)$(dig +short cloud.molejo.dev AAAA)" ]] || {
  echo "cloud.molejo.dev has no public A or AAAA answer" >&2
  exit 1
}
tls_status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' --max-time 10 https://cloud.molejo.dev/)"
[[ "$tls_status" != 000 ]] || { echo "public TLS handshake failed" >&2; exit 1; }

for reference in "$FRUTO_API_IMAGE" "$FRUTO_CONSOLE_IMAGE" "$FRUTO_TESTKIT_IMAGE"; do
  docker buildx imagetools inspect "$reference" >/dev/null
done

printf 'preflight ok: context=%s server=%s cluster_uid=%s nodes=Ready/amd64 gateway=Programmed certificate=Ready dns=resolved tls_http=%s images=resolved\n' \
  "$target_context" "$actual_server" "$actual_cluster_uid" "$tls_status"
