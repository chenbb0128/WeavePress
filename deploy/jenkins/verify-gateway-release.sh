#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

die() {
  printf 'verify-gateway-release: %s\n' "$1" >&2
  exit 1
}

[[ $# -ge 2 ]] || die 'expected APP_SHA and at least one HTTPS endpoint'
app_sha="$1"
shift
[[ "$app_sha" =~ ^[0-9a-f]{40}$ ]] || die 'APP_SHA must be a complete lowercase commit SHA'

for endpoint in "$@"; do
  [[ "$endpoint" =~ ^https://[^[:space:]]+$ ]] || die 'endpoint must use HTTPS'
  headers="$(mktemp)" || die 'could not allocate response header buffer'
  cleanup_headers() {
    rm -f -- "$headers"
  }
  trap cleanup_headers EXIT
  if ! http_code="$(curl --noproxy '*' --silent --show-error \
    --connect-timeout 5 --max-time 20 \
    --retry 5 --retry-all-errors --retry-delay 3 \
    --max-redirs 0 \
    --dump-header "$headers" \
    --output /dev/null \
    --write-out '%{http_code}' \
    "$endpoint")"; then
    die "request failed for ${endpoint}"
  fi
  [[ "$http_code" == 200 ]] || die "expected HTTP 200 from ${endpoint}"

  release_headers=()
  while IFS= read -r header_line; do
    header_line="${header_line%$'\r'}"
    [[ "$header_line" == *:* ]] || continue
    header_name="${header_line%%:*}"
    [[ "${header_name,,}" == x-weavepress-release ]] || continue
    header_value="${header_line#*:}"
    header_value="${header_value#"${header_value%%[![:space:]]*}"}"
    header_value="${header_value%"${header_value##*[![:space:]]}"}"
    release_headers+=("$header_value")
  done < "$headers"
  [[ "${#release_headers[@]}" -eq 1 ]] || \
    die "expected exactly one X-WeavePress-Release header from ${endpoint}"
  [[ "${release_headers[0]}" == "$app_sha" ]] || \
    die "X-WeavePress-Release does not match APP_SHA for ${endpoint}"
  rm -f -- "$headers"
  headers=""
  trap - EXIT
done

printf 'Gateway public release verified at %s.\n' "$app_sha"
