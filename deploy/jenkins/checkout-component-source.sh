#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

readonly HELPER_TIMEOUT_SECONDS=180
readonly CHECKOUT_TIMEOUT_SECONDS=120

die() {
  printf 'checkout-component-source: %s\n' "$1" >&2
  exit 1
}

[[ $# -eq 4 ]] || die 'expected repository, component, APP_SHA, and helper output path'
repository="$1"
component="$2"
app_sha="$3"
helper_output="$4"
[[ -d "$repository/.git" ]] || die 'no-checkout repository is missing'
[[ "$component" == server || "$component" == gateway ]] || die 'component must be server or gateway'
[[ "$app_sha" =~ ^[0-9a-f]{40}$ ]] || die 'APP_SHA must be a complete lowercase commit SHA'
[[ -n "$helper_output" && ! -d "$helper_output" ]] || die 'helper output path is invalid'

helper_temp="$(mktemp "${helper_output}.XXXXXX")" || die 'could not allocate freshness helper'
cleanup() {
  [[ -z "$helper_temp" ]] || rm -f -- "$helper_temp"
}
trap cleanup EXIT

timeout "$HELPER_TIMEOUT_SECONDS" git -C "$repository" show \
  'refs/remotes/origin/master:deploy/jenkins/assert-component-current.sh' > "$helper_temp" || \
  die 'could not extract freshness helper from origin/master'
chmod 0700 "$helper_temp"
timeout "$HELPER_TIMEOUT_SECONDS" bash "$helper_temp" "$repository" "$component" "$app_sha" || \
  die 'component freshness check failed'
timeout "$CHECKOUT_TIMEOUT_SECONDS" git -C "$repository" checkout --detach "$app_sha" >/dev/null || \
  die 'could not checkout requested APP_SHA'
actual_sha="$(git -C "$repository" rev-parse --verify HEAD)" || die 'could not resolve checked-out HEAD'
[[ "$actual_sha" == "$app_sha" ]] || die 'checked-out HEAD does not match APP_SHA'
mv -f -- "$helper_temp" "$helper_output"
helper_temp=""
