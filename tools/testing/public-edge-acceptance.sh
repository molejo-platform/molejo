#!/usr/bin/env bash
# Read-only public edge acceptance. It never uses kubectl or changes DNS/routes.
set -uo pipefail

usage() {
  echo "usage: $0 --output <directory> [--apex-host <host>] [--apex-body-marker <text>] [--console-host <host>] [--console-body-marker <text>] [--registry-host <host>] [--minimum-certificate-days <days>]" >&2
  exit 2
}

apex_host="molejo.dev"
console_host="cloud.molejo.dev"
registry_host="registry.molejo.dev"
minimum_certificate_days=14
output_directory=""
apex_body_marker=""
console_body_marker=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apex-host) [[ $# -ge 2 ]] || usage; apex_host="$2"; shift 2 ;;
    --apex-body-marker) [[ $# -ge 2 ]] || usage; apex_body_marker="$2"; shift 2 ;;
    --console-host) [[ $# -ge 2 ]] || usage; console_host="$2"; shift 2 ;;
    --console-body-marker) [[ $# -ge 2 ]] || usage; console_body_marker="$2"; shift 2 ;;
    --registry-host) [[ $# -ge 2 ]] || usage; registry_host="$2"; shift 2 ;;
    --minimum-certificate-days) [[ $# -ge 2 ]] || usage; minimum_certificate_days="$2"; shift 2 ;;
    --output) [[ $# -ge 2 ]] || usage; output_directory="$2"; shift 2 ;;
    *) usage ;;
  esac
done

[[ -n "$output_directory" && "$minimum_certificate_days" =~ ^[0-9]+$ ]] || usage
for command_name in curl jq openssl; do
  command -v "$command_name" >/dev/null || {
    echo "required command not found: $command_name" >&2
    exit 2
  }
done
for hostname in "$apex_host" "$console_host" "$registry_host"; do
  [[ "$hostname" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || {
    echo "invalid public hostname: $hostname" >&2
    exit 2
  }
done

mkdir -p "$output_directory" || { echo "cannot create output directory" >&2; exit 2; }
chmod 700 "$output_directory" || { echo "cannot protect output directory" >&2; exit 2; }
items_file="$(mktemp "${TMPDIR:-/tmp}/molejo-public-edge.XXXXXX")"
body_file="$(mktemp "${TMPDIR:-/tmp}/molejo-public-body.XXXXXX")"
certificate_file="$(mktemp "${TMPDIR:-/tmp}/molejo-public-cert.XXXXXX")"
certificate_chain_file="$(mktemp "${TMPDIR:-/tmp}/molejo-public-chain.XXXXXX")"
headers_file="$(mktemp "${TMPDIR:-/tmp}/molejo-public-headers.XXXXXX")"
openssl_pid=""
watchdog_pid=""
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
status="FAIL"
reason="acceptance interrupted"

write_report() {
  local finished_at
  finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  jq -s \
    --arg schemaVersion "molejo-public-edge-acceptance.v1alpha1" \
    --arg status "$status" \
    --arg reason "$reason" \
    --arg startedAt "$started_at" \
    --arg finishedAt "$finished_at" \
    '{schemaVersion:$schemaVersion,status:$status,reason:$reason,startedAt:$startedAt,finishedAt:$finishedAt,checks:.}' \
      "$items_file" >"${output_directory}/report.json.tmp" 2>/dev/null || return 1
    chmod 600 "${output_directory}/report.json.tmp" || return 1
    mv "${output_directory}/report.json.tmp" "${output_directory}/report.json"
}

cleanup() {
  local exit_status=$?
  trap - EXIT INT TERM
  [[ -n "$openssl_pid" ]] && kill "$openssl_pid" >/dev/null 2>&1 || true
  [[ -n "$watchdog_pid" ]] && kill "$watchdog_pid" >/dev/null 2>&1 || true
  if ! write_report; then
    rm -f -- "$items_file" "$body_file" "$certificate_file" "$certificate_chain_file" "$headers_file"
    echo "Public edge acceptance evidence could not be published" >&2
    exit 2
  fi
  rm -f -- "$items_file" "$body_file" "$certificate_file" "$certificate_chain_file" "$headers_file"
  if [[ "$status" != "PASS" ]]; then
    echo "Public edge acceptance failed: ${reason}" >&2
    echo "Acceptance evidence: ${output_directory}" >&2
    [[ $exit_status -ne 0 ]] || exit_status=1
    exit "$exit_status"
  fi
  echo "Public edge acceptance passed; evidence: ${output_directory}"
  exit "$exit_status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

probe_https() {
  local name="$1" hostname="$2" path="$3" expected_status="$4" body_marker="$5" required_header="$6"
  local observed_status observed_status_number fingerprint valid_until body_sha256 header_matched=false check_status="PASS" check_reason=""
  : >"$body_file"
  : >"$headers_file"
  observed_status="$(curl --silent --show-error --output "$body_file" --write-out '%{http_code}' \
      --dump-header "$headers_file" --connect-timeout 5 --max-time 20 --max-redirs 0 "https://${hostname}${path}" 2>/dev/null)" || observed_status="000"
  if [[ "$observed_status" != "$expected_status" ]]; then
    check_status="FAIL"
    check_reason="unexpected HTTP status"
  fi
  body_sha256="$(openssl dgst -sha256 "$body_file" 2>/dev/null | sed -E 's/^.*= //')"
  if [[ -n "$body_marker" ]] && ! grep -Fq -- "$body_marker" "$body_file"; then
    check_status="FAIL"
    check_reason="expected body marker absent"
  fi
  if [[ -n "$required_header" ]]; then
    if grep -Eqi -- "$required_header" "$headers_file"; then
      header_matched=true
    else
      check_status="FAIL"
      check_reason="required response header absent"
    fi
  fi

  : >"$certificate_file"
  : >"$certificate_chain_file"
  openssl s_client -connect "${hostname}:443" -servername "$hostname" -showcerts </dev/null >"$certificate_chain_file" 2>/dev/null &
  openssl_pid=$!
  ( sleep 20; kill "$openssl_pid" >/dev/null 2>&1 || true ) &
  watchdog_pid=$!
  wait "$openssl_pid" >/dev/null 2>&1 || true
  openssl_pid=""
  kill "$watchdog_pid" >/dev/null 2>&1 || true
  wait "$watchdog_pid" >/dev/null 2>&1 || true
  watchdog_pid=""
  openssl x509 -outform PEM <"$certificate_chain_file" >"$certificate_file" 2>/dev/null || true
  if [[ ! -s "$certificate_file" ]]; then
    check_status="FAIL"
    check_reason="certificate unavailable"
    fingerprint=""
    valid_until=""
  else
    fingerprint="$(openssl x509 -in "$certificate_file" -noout -fingerprint -sha256 2>/dev/null | sed -E 's/^[^=]+=//; s/://g' | tr '[:upper:]' '[:lower:]')"
    valid_until="$(openssl x509 -in "$certificate_file" -noout -enddate 2>/dev/null | sed 's/^notAfter=//')"
    if ! openssl x509 -in "$certificate_file" -noout -checkend "$((minimum_certificate_days * 86400))" >/dev/null 2>&1; then
      check_status="FAIL"
      check_reason="certificate validity below minimum"
    fi
  fi
  if [[ "$observed_status" =~ ^[0-9]{3}$ ]]; then
    observed_status_number="$((10#$observed_status))"
  else
    observed_status_number=0
  fi

  if ! jq -n --arg name "$name" --arg hostname "$hostname" --arg status "$check_status" \
    --arg reason "$check_reason" --argjson expectedHTTPStatus "$expected_status" \
    --argjson observedHTTPStatus "$observed_status_number" --arg certificateSHA256 "$fingerprint" \
      --arg certificateValidUntil "$valid_until" --arg bodySHA256 "$body_sha256" --argjson requiredHeaderMatched "$header_matched" \
      '{name:$name,hostname:$hostname,status:$status,reason:$reason,expectedHTTPStatus:$expectedHTTPStatus,observedHTTPStatus:$observedHTTPStatus,bodySHA256:$bodySHA256,requiredHeaderMatched:$requiredHeaderMatched,certificateSHA256:$certificateSHA256,certificateValidUntil:$certificateValidUntil}' >>"$items_file"; then
    status="BLOCKED"
    reason="acceptance evidence could not record a check"
    exit 2
  fi
  [[ "$check_status" == "PASS" ]]
}

if ! probe_https apex "$apex_host" / 200 "$apex_body_marker" ""; then reason="apex acceptance failed"; exit 1; fi
if ! probe_https console "$console_host" / 200 "$console_body_marker" ""; then reason="Console acceptance failed"; exit 1; fi
if ! probe_https registry "$registry_host" /v2/ 401 "" '^www-authenticate:[[:space:]]*Bearer'; then reason="Registry acceptance failed"; exit 1; fi

status="PASS"
reason=""
