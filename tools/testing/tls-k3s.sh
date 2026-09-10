#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 verify --context <name> --file <tls-setup.yaml>" >&2
  exit 2
}

[[ $# -ge 1 ]] || usage
mode="$1"
shift
context_name=""
setup_path=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context)
      [[ $# -ge 2 ]] || usage
      context_name="$2"
      shift 2
      ;;
    --file)
      [[ $# -ge 2 ]] || usage
      setup_path="$2"
      shift 2
      ;;
    *) usage ;;
  esac
done

[[ -n "$context_name" ]] || usage
[[ -n "$setup_path" ]] || usage
kubectl config get-contexts "$context_name" >/dev/null
repository_root="$(git rev-parse --show-toplevel)"

verify() {
  if [[ -n "${MOLEJOCTL_BIN:-}" ]]; then
    "$MOLEJOCTL_BIN" capability tls verify --kube-context "$context_name" --file "$setup_path"
    return
  fi
  (
    cd "$repository_root"
    go run ./apps/molejoctl capability tls verify --kube-context "$context_name" --file "$setup_path"
  )
}

case "$mode" in
  verify) verify ;;
  *) usage ;;
esac
