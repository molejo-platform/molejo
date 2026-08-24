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

umask 077
mkdir -p "$release_dir/secrets"
owner_password="$release_dir/secrets/owner-password"
owner_hash="$release_dir/secrets/owner-password-hash"
postgres_user="$release_dir/secrets/postgres-username"
postgres_password="$release_dir/secrets/postgres-password"
postgres_database="$release_dir/secrets/postgres-database"
database_url="$release_dir/secrets/database-url"

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
chmod 0600 "$release_dir"/secrets/*

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

printf 'prepared external Secrets; owner password remains only at %s\n' "$owner_password"
