#!/usr/bin/env bash
set -euo pipefail

for command in go jq kubectl openssl install mkdir mktemp cmp grep; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR to a directory outside the checkout}"
case "$release_dir" in
  "$PWD"/*) echo "release secrets must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_DIR must be an absolute path" >&2; exit 2;;
esac

actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"

umask 077
mkdir -p "$release_dir/secrets"
owner_password="$release_dir/secrets/owner-password"
owner_hash="$release_dir/secrets/owner-password-hash"
postgres_user="$release_dir/secrets/postgres-username"
postgres_password="$release_dir/secrets/postgres-password"
postgres_database="$release_dir/secrets/postgres-database"
database_url="$release_dir/secrets/database-url"
password_reset_key="$release_dir/secrets/password-reset-key"
agent_ca_certificate="$release_dir/secrets/agent-ca.crt"
agent_ca_key="$release_dir/secrets/agent-ca.key"
agent_server_certificate="$release_dir/secrets/agent-server.crt"
agent_server_key="$release_dir/secrets/agent-server.key"

if [[ ! -s "$owner_password" ]]; then
  openssl rand -base64 24 >"$owner_password"
fi
if [[ ! -s "$owner_hash" ]]; then
  go run ./services/control-plane-api/cmd/control-plane-api hash-password <"$owner_password" >"$owner_hash"
fi
if [[ ! -s "$postgres_user" ]]; then printf '%s' fruto >"$postgres_user"; fi
if [[ ! -s "$postgres_database" ]]; then printf '%s' fruto >"$postgres_database"; fi
if [[ ! -s "$postgres_password" ]]; then openssl rand -hex 32 >"$postgres_password"; fi
if [[ ! -s "$database_url" ]]; then
  printf 'postgresql://fruto:%s@postgres:5432/fruto?sslmode=disable' "$(<"$postgres_password")" >"$database_url"
fi
if [[ ! -s "$password_reset_key" ]]; then openssl rand -base64 48 >"$password_reset_key"; fi
chmod 0600 "$release_dir"/secrets/*

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT
kubectl --context "$context" apply -f deploy/control-plane/namespace.yaml >/dev/null
server_hostname="control-plane-api.fruto-control-plane.svc.cluster.local"
if [[ -e "$agent_ca_certificate" || -e "$agent_ca_key" ]]; then
  [[ -s "$agent_ca_certificate" && -s "$agent_ca_key" ]] || { echo "Agent CA material is incomplete" >&2; exit 1; }
else
  openssl ecparam -name prime256v1 -genkey -noout -out "$agent_ca_key"
  openssl req -x509 -new -sha256 -key "$agent_ca_key" -days 3650 \
    -subj '/CN=Molejo Cluster Agent CA' -out "$agent_ca_certificate"
fi
openssl x509 -in "$agent_ca_certificate" -pubkey -noout >"$temporary_dir/agent-ca-cert.pub"
openssl pkey -in "$agent_ca_key" -pubout >"$temporary_dir/agent-ca-key.pub"
cmp -s "$temporary_dir/agent-ca-cert.pub" "$temporary_dir/agent-ca-key.pub" || { echo "Agent CA certificate does not match its key" >&2; exit 1; }

regenerate_server=false
if [[ -e "$agent_server_certificate" || -e "$agent_server_key" ]]; then
  [[ -s "$agent_server_certificate" && -s "$agent_server_key" ]] || { echo "Agent server TLS material is incomplete" >&2; exit 1; }
  openssl verify -CAfile "$agent_ca_certificate" "$agent_server_certificate" >/dev/null 2>&1 || regenerate_server=true
  openssl x509 -checkend 86400 -noout -in "$agent_server_certificate" >/dev/null 2>&1 || regenerate_server=true
  openssl x509 -in "$agent_server_certificate" -noout -ext subjectAltName 2>/dev/null | grep -Fq "DNS:$server_hostname" || regenerate_server=true
else
  regenerate_server=true
fi
if [[ "$regenerate_server" == true ]]; then
  openssl ecparam -name prime256v1 -genkey -noout -out "$agent_server_key"
  openssl req -new -sha256 -key "$agent_server_key" -subj '/CN=Molejo Cluster Agent gRPC' \
    -out "$temporary_dir/agent-server.csr"
  printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\nkeyUsage=digitalSignature,keyAgreement\n' "$server_hostname" >"$temporary_dir/agent-server.ext"
  serial="$(openssl rand -hex 16)"
  openssl x509 -req -sha256 -in "$temporary_dir/agent-server.csr" \
    -CA "$agent_ca_certificate" -CAkey "$agent_ca_key" -set_serial "0x$serial" -days 825 \
    -extfile "$temporary_dir/agent-server.ext" -out "$agent_server_certificate" >/dev/null 2>&1
fi
chmod 0600 "$agent_ca_key" "$agent_server_key"
chmod 0644 "$agent_ca_certificate" "$agent_server_certificate"

kubectl --context "$context" -n fruto-control-plane create secret generic molejo-password-reset \
  --from-file=key="$password_reset_key" --dry-run=client -o json >"$temporary_dir/password-reset.json"
password_reset_secret="$(apply_versioned_object "$context" fruto-control-plane molejo-password-reset password-reset "$temporary_dir/password-reset.json")"

kubectl --context "$context" -n fruto-control-plane create secret generic molejo-agent-ca \
  --from-file=ca.crt="$agent_ca_certificate" --from-file=ca.key="$agent_ca_key" \
  --dry-run=client -o json >"$temporary_dir/agent-ca.json"
agent_ca_secret="$(apply_versioned_object "$context" fruto-control-plane molejo-agent-ca agent-ca "$temporary_dir/agent-ca.json")"
kubectl --context "$context" -n fruto-control-plane create secret tls molejo-agent-server \
  --cert="$agent_server_certificate" --key="$agent_server_key" \
  --dry-run=client -o json >"$temporary_dir/agent-server.json"
agent_server_tls_secret="$(apply_versioned_object "$context" fruto-control-plane molejo-agent-server agent-server-tls "$temporary_dir/agent-server.json")"

kubectl --context "$context" -n fruto-control-plane create secret generic fruto-control-plane-postgres \
  --from-file=username="$postgres_user" \
  --from-file=password="$postgres_password" \
  --from-file=database="$postgres_database" \
  --dry-run=client -o json | kubectl --context "$context" apply -f - >/dev/null
kubectl --context "$context" -n fruto-control-plane create secret generic fruto-control-plane-db \
  --from-file=database-url="$database_url" \
  --dry-run=client -o json | kubectl --context "$context" apply -f - >/dev/null
kubectl --context "$context" -n fruto-control-plane create secret generic fruto-control-plane-bootstrap \
  --from-file=FRUTO_OWNER_PASSWORD_HASH="$owner_hash" \
  --dry-run=client -o json | kubectl --context "$context" apply -f - >/dev/null
kubectl --context "$context" -n fruto-system get secret registry-pull -o json |
  jq 'del(.metadata.annotations,.metadata.creationTimestamp,.metadata.managedFields,.metadata.ownerReferences,.metadata.resourceVersion,.metadata.uid) | .metadata.namespace="fruto-control-plane"' |
  kubectl --context "$context" apply -f - >/dev/null
if kubectl --context "$context" -n fruto-control-plane get secret molejo-github-app >/dev/null 2>&1; then
  github_secret="$(copy_versioned_secret "$context" fruto-control-plane molejo-github-app molejo-github-app github-app)"
else
  github_secret="molejo-github-app-unconfigured"
fi
webhook_secret_file="${MOLEJO_GITHUB_WEBHOOK_SECRET_FILE:-}"
if [[ -n "$webhook_secret_file" ]]; then
  [[ -s "$webhook_secret_file" ]] || { echo "MOLEJO_GITHUB_WEBHOOK_SECRET_FILE does not exist" >&2; exit 2; }
  webhook_input="$temporary_dir/github-webhook.json"
  kubectl --context "$context" -n fruto-control-plane create secret generic molejo-github-webhook \
    --from-file=GITHUB_WEBHOOK_SECRET="$webhook_secret_file" --dry-run=client -o json >"$webhook_input"
  webhook_secret="$(apply_versioned_object "$context" fruto-control-plane molejo-github-webhook github-webhook "$webhook_input")"
else
  webhook_secret="$(kubectl --context "$context" -n fruto-control-plane get secret \
    -l molejo.dev/configuration-family=github-webhook -o json | jq -er '.items | sort_by(.metadata.creationTimestamp) | last | .metadata.name')" || {
      echo "set MOLEJO_GITHUB_WEBHOOK_SECRET_FILE to the protected webhook secret file" >&2
      exit 2
    }
fi
release_metadata_write "$release_dir/metadata/control-plane.env" \
  "MOLEJO_GITHUB_APP_SECRET=$github_secret" \
  "MOLEJO_GITHUB_WEBHOOK_SECRET=$webhook_secret" \
  "MOLEJO_PASSWORD_RESET_SECRET=$password_reset_secret" \
  "MOLEJO_AGENT_CA_SECRET=$agent_ca_secret" \
  "MOLEJO_AGENT_SERVER_TLS_SECRET=$agent_server_tls_secret"

printf 'prepared external Secrets and immutable GitHub credential version; owner password remains only at %s\n' "$owner_password"
