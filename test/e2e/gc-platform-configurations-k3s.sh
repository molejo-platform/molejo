#!/usr/bin/env bash
set -euo pipefail

for command in kubectl jq sort cut awk mktemp grep; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 2; }
done
context="${FRUTO_K3S_CONTEXT:-fruto-lab}"
[[ "$context" == "fruto-lab" ]] || { echo "configuration GC requires context fruto-lab" >&2; exit 2; }
expected_uid="${FRUTO_EXPECTED_CLUSTER_UID:?set FRUTO_EXPECTED_CLUSTER_UID}"
[[ "${MOLEJO_CONFIRM_CONFIGURATION_GC:-}" == "retain-two-unreferenced" ]] || {
  echo "set MOLEJO_CONFIRM_CONFIGURATION_GC=retain-two-unreferenced after accepting all active releases" >&2
  exit 2
}
actual_uid="$(kubectl --context "$context" get namespace kube-system -o jsonpath='{.metadata.uid}')"
[[ "$actual_uid" == "$expected_uid" ]] || { echo "cluster UID does not match the approved target" >&2; exit 1; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/release-configuration.sh"
for namespace in molejo-observability molejo-observability-agents fruto-control-plane molejo-builds molejo-secrets; do
  garbage_collect_versioned_objects "$context" "$namespace"
done
printf 'unreferenced platform configuration versions collected on %s; two newest versions per family retained\n' "$context"
