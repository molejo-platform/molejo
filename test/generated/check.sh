#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "${temporary_directory}"' EXIT

generated_files=(
  "deploy/crds/platform.fruto.calouro.tech_appdeployments.yaml"
  "deploy/operator/rbac/role.yaml"
  "packages/kubernetes-api/apis/platform/v1alpha1/zz_generated.deepcopy.go"
)

cd "${repo_root}"
for generated_file in "${generated_files[@]}"; do
  mkdir -p "${temporary_directory}/$(dirname "${generated_file}")"
  cp "${generated_file}" "${temporary_directory}/${generated_file}"
done

just generate

for generated_file in "${generated_files[@]}"; do
  if ! cmp -s "${temporary_directory}/${generated_file}" "${generated_file}"; then
    echo "Generated file is stale: ${generated_file}" >&2
    exit 1
  fi
done
