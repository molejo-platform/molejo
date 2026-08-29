#!/usr/bin/env bash
set -euo pipefail

for command in go jq kubectl openssl install mkdir mktemp; do
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
kubectl --context "$context" -n fruto-control-plane create secret generic molejo-password-reset \
  --from-file=key="$password_reset_key" --dry-run=client -o json >"$temporary_dir/password-reset.json"
password_reset_secret="$(apply_versioned_object "$context" fruto-control-plane molejo-password-reset password-reset "$temporary_dir/password-reset.json")"

kubectl --context "$context" apply -f deploy/control-plane/namespace.yaml >/dev/null
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
  "MOLEJO_PASSWORD_RESET_SECRET=$password_reset_secret"

printf 'prepared external Secrets and immutable GitHub credential version; owner password remains only at %s\n' "$owner_password"
