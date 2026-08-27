#!/usr/bin/env bash
set -euo pipefail

for command in grep jq kubectl openssl sed mktemp; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
github_app_id="${GITHUB_APP_ID:?set GITHUB_APP_ID}"
github_key_file="${GITHUB_APP_PRIVATE_KEY_FILE:?set GITHUB_APP_PRIVATE_KEY_FILE}"
release_dir="${FRUTO_RELEASE_DIR:?set FRUTO_RELEASE_DIR to a directory outside the checkout}"
registry_secret_namespace="${MOLEJO_REGISTRY_SECRET_NAMESPACE:-fruto-system}"
registry_secret_name="${MOLEJO_REGISTRY_SECRET_NAME:-registry-pull}"
[[ -s "$github_key_file" ]] || { echo "required GitHub private key file is missing" >&2; exit 2; }
case "$release_dir" in
  "$PWD"/*) echo "release secrets must remain outside the repository" >&2; exit 2;;
  /*) ;;
  *) echo "FRUTO_RELEASE_DIR must be an absolute path" >&2; exit 2;;
esac

actual_uid="$(kubectl --context fruto-lab get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

umask 077
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
database_url_file="${MOLEJO_BUILD_DATABASE_URL_FILE:-$temporary/database-url}"
if [[ -z "${MOLEJO_BUILD_DATABASE_URL_FILE:-}" ]]; then
  kubectl --context fruto-lab -n fruto-control-plane get secret fruto-control-plane-db -o jsonpath='{.data.database-url}' |
    openssl base64 -d -A |
    sed 's/@postgres:/@postgres.fruto-control-plane.svc:/' >"$database_url_file"
fi
[[ -s "$database_url_file" ]] || { echo "build database URL file is missing" >&2; exit 2; }
grep -q '@postgres\.fruto-control-plane\.svc:5432/' "$database_url_file" || { echo "build database URL must target postgres.fruto-control-plane.svc:5432" >&2; exit 2; }

tls_dir="$release_dir/secrets/buildkit-tls"
mkdir -p "$tls_dir"
tls_ready=true
for tls_file in ca.pem ca-key.pem server-cert.pem server-key.pem client-cert.pem client-key.pem; do
  [[ -s "$tls_dir/$tls_file" ]] || tls_ready=false
done
if [[ "$tls_ready" != true ]]; then
  openssl req -x509 -newkey rsa:3072 -nodes -days 365 -subj '/CN=Molejo BuildKit CA' -keyout "$tls_dir/ca-key.pem" -out "$tls_dir/ca.pem" >/dev/null 2>&1
  openssl req -newkey rsa:3072 -nodes -subj '/CN=buildkitd.molejo-builds.svc' -keyout "$tls_dir/server-key.pem" -out "$tls_dir/server.csr" >/dev/null 2>&1
  openssl x509 -req -days 365 -in "$tls_dir/server.csr" -CA "$tls_dir/ca.pem" -CAkey "$tls_dir/ca-key.pem" -CAcreateserial -out "$tls_dir/server-cert.pem" -extfile <(printf 'subjectAltName=DNS:buildkitd,DNS:buildkitd.molejo-builds,DNS:buildkitd.molejo-builds.svc\nextendedKeyUsage=serverAuth\n') >/dev/null 2>&1
  openssl req -newkey rsa:3072 -nodes -subj '/CN=molejo-build-worker' -keyout "$tls_dir/client-key.pem" -out "$tls_dir/client.csr" >/dev/null 2>&1
  openssl x509 -req -days 365 -in "$tls_dir/client.csr" -CA "$tls_dir/ca.pem" -CAkey "$tls_dir/ca-key.pem" -CAcreateserial -out "$tls_dir/client-cert.pem" -extfile <(printf 'extendedKeyUsage=clientAuth\n') >/dev/null 2>&1
fi
chmod 0600 "$tls_dir"/*

kubectl --context fruto-lab apply -f deploy/builds/namespace.yaml >/dev/null
kubectl --context fruto-lab -n molejo-builds create secret generic molejo-build-worker \
  --from-file=database-url="$database_url_file" \
  --from-literal=github-app-id="$github_app_id" \
  --from-file=github-private-key.pem="$github_key_file" \
  --dry-run=client -o json | kubectl --context fruto-lab apply -f - >/dev/null
kubectl --context fruto-lab -n molejo-builds create secret generic molejo-buildkit-tls \
  --from-file=ca.pem="$tls_dir/ca.pem" \
  --from-file=server-cert.pem="$tls_dir/server-cert.pem" \
  --from-file=server-key.pem="$tls_dir/server-key.pem" \
  --from-file=client-cert.pem="$tls_dir/client-cert.pem" \
  --from-file=client-key.pem="$tls_dir/client-key.pem" \
  --dry-run=client -o json | kubectl --context fruto-lab apply -f - >/dev/null
kubectl --context fruto-lab -n "$registry_secret_namespace" get secret "$registry_secret_name" -o json |
  jq 'del(.metadata.annotations,.metadata.creationTimestamp,.metadata.managedFields,.metadata.ownerReferences,.metadata.resourceVersion,.metadata.uid) | .metadata.name="molejo-build-registry" | .metadata.namespace="molejo-builds"' |
  kubectl --context fruto-lab apply -f - >/dev/null

printf 'prepared external build-plane Secrets in context fruto-lab; private material remains under %s\n' "$tls_dir"
