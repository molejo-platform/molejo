#!/usr/bin/env bash
set -euo pipefail

for command in grep kubectl; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done

expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
release_file="${FRUTO_BUILDS_RELEASE_OUTPUT:?set FRUTO_BUILDS_RELEASE_OUTPUT to the rendered build-plane manifest}"
[[ -s "$release_file" ]] || { echo "build release manifest does not exist" >&2; exit 2; }
! grep -Eq 'sha256:0{64}|CHANGE_ME' "$release_file" || { echo "build release manifest contains placeholders" >&2; exit 2; }

actual_uid="$(kubectl --context fruto-lab get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }
for secret in molejo-build-worker molejo-buildkit-tls molejo-build-registry; do
  kubectl --context fruto-lab -n molejo-builds get secret "$secret" >/dev/null
done

kubectl --context fruto-lab apply --server-side --dry-run=server -f "$release_file" >/dev/null
kubectl --context fruto-lab apply --server-side -f "$release_file" >/dev/null
kubectl --context fruto-lab -n molejo-builds rollout status deployment/buildkitd --timeout=300s
kubectl --context fruto-lab -n molejo-builds rollout status deployment/build-worker --timeout=300s
printf 'build plane release applied to context fruto-lab\n'
