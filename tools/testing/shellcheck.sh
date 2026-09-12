#!/usr/bin/env bash
set -euo pipefail

readonly shellcheck_version="v0.11.0"
readonly release_base="https://github.com/koalaman/shellcheck/releases/download/${shellcheck_version}"

operating_system="$(uname -s)"
architecture="$(uname -m)"
case "${operating_system}/${architecture}" in
  Darwin/arm64)
    platform="darwin.aarch64"
    expected_sha256="339b930feb1ea764467013cc1f72d09cd6b869ebf1013296ba9055ab2ffbd26f"
    ;;
  Darwin/x86_64)
    platform="darwin.x86_64"
    expected_sha256="c2c15e08df0e8fbc374c335b230a7ee958c313fa5714817a59aa59f1aa594f51"
    ;;
  Linux/aarch64 | Linux/arm64)
    platform="linux.aarch64"
    expected_sha256="68a8133197a50beb8803f8d42f9908d1af1c5540d4bb05fdfca8c1fa47decefc"
    ;;
  Linux/x86_64 | Linux/amd64)
    platform="linux.x86_64"
    expected_sha256="b7af85e41cc99489dcc21d66c6d5f3685138f06d34651e6d34b42ec6d54fe6f6"
    ;;
  *)
    echo "unsupported ShellCheck platform: ${operating_system}/${architecture}" >&2
    exit 2
    ;;
esac

tool_cache="${MOLEJO_TOOL_CACHE:-${TMPDIR:-/tmp}/molejo-tool-cache}"
install_directory="${tool_cache}/shellcheck/${shellcheck_version}/${platform}"
shellcheck_binary="${install_directory}/shellcheck"

if [[ ! -x "$shellcheck_binary" ]]; then
  for command_name in curl tar; do
    command -v "$command_name" >/dev/null || {
      echo "required command not found: $command_name" >&2
      exit 2
    }
  done
  if ! command -v sha256sum >/dev/null && ! command -v shasum >/dev/null; then
    echo "sha256sum or shasum is required to verify ShellCheck" >&2
    exit 2
  fi

  temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/molejo-shellcheck.XXXXXX")"
  trap 'rm -rf -- "$temporary_directory"' EXIT
  archive="${temporary_directory}/shellcheck.tar.gz"
  artifact="shellcheck-${shellcheck_version}.${platform}.tar.gz"
  curl --fail --location --silent --show-error \
    "${release_base}/${artifact}" \
    --output "$archive"
  if command -v sha256sum >/dev/null; then
    observed_sha256="$(sha256sum "$archive" | awk '{print $1}')"
  else
    observed_sha256="$(shasum -a 256 "$archive" | awk '{print $1}')"
  fi
  if [[ "$observed_sha256" != "$expected_sha256" ]]; then
    echo "ShellCheck archive checksum mismatch" >&2
    exit 1
  fi

  tar -xzf "$archive" -C "$temporary_directory"
  mkdir -p "$install_directory"
  install -m 0755 \
    "${temporary_directory}/shellcheck-${shellcheck_version}/shellcheck" \
    "${shellcheck_binary}.tmp"
  mv "${shellcheck_binary}.tmp" "$shellcheck_binary"
fi

exec "$shellcheck_binary" "$@"
