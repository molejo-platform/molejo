#!/usr/bin/env bash
set -euo pipefail

for command in kubectl openssl mkdir chmod cat tr jq install mktemp; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
[[ "$context" == "fruto-lab" ]] || { echo "observability lab preparation requires context fruto-lab" >&2; exit 2; }
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR to a directory outside the checkout}"
case "$release_dir" in
  "$PWD"/*) echo "observability credentials must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_DIR must be an absolute path" >&2; exit 2;;
esac

actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"

umask 077
credential_dir="$release_dir/secrets/observability"
mkdir -p "$credential_dir"
ingest_password="$credential_dir/ingest-password"
reader_password="$credential_dir/reader-password"
users_xml="$credential_dir/users.xml"
[[ -s "$ingest_password" ]] || { openssl rand -hex 32 | tr -d '\r\n' >"$ingest_password"; }
[[ -s "$reader_password" ]] || { openssl rand -hex 32 | tr -d '\r\n' >"$reader_password"; }
ingest_value="$(tr -d '\r\n' <"$ingest_password")"
reader_value="$(tr -d '\r\n' <"$reader_password")"
printf '%s' "$ingest_value" >"$ingest_password"
printf '%s' "$reader_value" >"$reader_password"
ingest_hash="$(openssl dgst -sha256 -r "$ingest_password")"
ingest_hash="${ingest_hash%% *}"
reader_hash="$(openssl dgst -sha256 -r "$reader_password")"
reader_hash="${reader_hash%% *}"
cat >"$users_xml" <<EOF
<clickhouse>
  <users>
    <default remove="remove"/>
    <molejo_ingest>
      <password_sha256_hex>${ingest_hash}</password_sha256_hex>
      <networks><ip>::/0</ip></networks>
      <profile>default</profile><quota>default</quota><access_management>0</access_management>
    </molejo_ingest>
    <molejo_reader>
      <password_sha256_hex>${reader_hash}</password_sha256_hex>
      <networks><ip>::/0</ip></networks>
      <profile>readonly</profile><quota>default</quota><access_management>0</access_management>
    </molejo_reader>
  </users>
</clickhouse>
EOF
chmod 0600 "$credential_dir"/*

kubectl --context "$context" apply -f deploy/observability/namespace.yaml >/dev/null
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
kubectl --context "$context" -n molejo-observability create secret generic molejo-observability-credentials \
  --from-file=ingest-password="$ingest_password" \
  --from-file=reader-password="$reader_password" \
  --from-file=users.xml="$users_xml" \
  --dry-run=client -o json >"$temporary/credentials.json"
credentials_secret="$(apply_versioned_object "$context" molejo-observability molejo-observability-credentials observability-credentials "$temporary/credentials.json")"
kubectl --context "$context" -n fruto-control-plane create secret generic molejo-observability-reader \
  --from-file=reader-password="$reader_password" \
  --dry-run=client -o json >"$temporary/reader.json"
reader_secret="$(apply_versioned_object "$context" fruto-control-plane molejo-observability-reader observability-reader "$temporary/reader.json")"
release_metadata_write "$release_dir/metadata/observability.env" \
  "MOLEJO_OBSERVABILITY_CREDENTIALS_SECRET=$credentials_secret" \
  "MOLEJO_OBSERVABILITY_READER_SECRET=$reader_secret"

printf 'prepared immutable observability credential versions; private material remains at %s\n' "$credential_dir"
