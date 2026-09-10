#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <verify|teardown> --context <name> [--profile <name>] [--confirm <name>]" >&2
  exit 2
}

[[ $# -ge 1 ]] || usage
mode="$1"
shift
context_name=""
profile_name="default"
confirmation=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context)
      [[ $# -ge 2 ]] || usage
      context_name="$2"
      shift 2
      ;;
    --profile)
      [[ $# -ge 2 ]] || usage
      profile_name="$2"
      shift 2
      ;;
    --confirm)
      [[ $# -ge 2 ]] || usage
      confirmation="$2"
      shift 2
      ;;
    *) usage ;;
  esac
done

[[ -n "$context_name" ]] || usage
kubectl config get-contexts "$context_name" >/dev/null
binding_name="molejo-tls-${profile_name}"

verify() {
  kubectl --context "$context_name" -n molejo-system get configmap "$binding_name" >/dev/null
  local secret_namespace secret_name
  secret_namespace="$(kubectl --context "$context_name" -n molejo-system get configmap "$binding_name" -o jsonpath='{.data.binding\.yaml}' | awk '/namespace:/ {print $2; exit}')"
  secret_name="$(kubectl --context "$context_name" -n molejo-system get configmap "$binding_name" -o jsonpath='{.data.binding\.yaml}' | awk '/name:/ {seen++; if (seen == 2) {print $2; exit}}')"
  if [[ -z "$secret_namespace" || -z "$secret_name" ]]; then
    echo "TLS profile $profile_name does not contain a valid Secret reference" >&2
    return 1
  fi
  kubectl --context "$context_name" -n "$secret_namespace" get secret "$secret_name" >/dev/null
  echo "TLS profile $profile_name is configured in context $context_name"
}

teardown() {
  [[ "$confirmation" == "$context_name" ]] || {
    echo "teardown requires --confirm $context_name" >&2
    exit 2
  }
  if kubectl --context "$context_name" get crd certificates.cert-manager.io >/dev/null 2>&1; then
    kubectl --context "$context_name" delete certificates -A -l "platform.molejo.dev/tls-profile=${profile_name}" --ignore-not-found
    kubectl --context "$context_name" delete clusterissuers -l "platform.molejo.dev/tls-profile=${profile_name}" --ignore-not-found
    kubectl --context "$context_name" delete secrets -A -l "platform.molejo.dev/tls-profile=${profile_name}" --ignore-not-found
    kubectl --context "$context_name" -n cert-manager delete secret "molejo-${profile_name}-staging-account-key" "molejo-${profile_name}-production-account-key" --ignore-not-found
  fi
  kubectl --context "$context_name" -n molejo-system delete configmap "$binding_name" --ignore-not-found
  echo "Molejo-managed resources for TLS profile $profile_name were removed from context $context_name"
}

case "$mode" in
  verify) verify ;;
  teardown) teardown ;;
  *) usage ;;
esac
