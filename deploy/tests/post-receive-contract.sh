#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_hook="$repo_root/deploy/nas/post-receive"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'NAS hook behavior violation: %s\n' "$1" >&2
  exit 1
}

bare="$test_root/WeavePress.git"
work="$test_root/work"
mock_bin="$test_root/bin"
state="$test_root/state"
secrets="$test_root/secrets"
enable="$test_root/enable"
hook_log="$test_root/post-receive.log"
lock_file="$test_root/post-receive.lock"
mkdir -p "$mock_bin" "$state" "$secrets"
git init --bare "$bare" >/dev/null
git init "$work" >/dev/null
git -C "$work" config user.name 'CI contract'
git -C "$work" config user.email 'ci-contract@example.invalid'
mkdir -p "$work/server"
printf 'old\n' > "$work/server/value.txt"
git -C "$work" add .
git -C "$work" commit -m old >/dev/null
oldrev="$(git -C "$work" rev-parse HEAD)"
mkdir -p "$work/admin"
printf 'new\n' > "$work/server/value.txt"
printf 'new\n' > "$work/admin/value.txt"
git -C "$work" add .
git -C "$work" commit -m new >/dev/null
newrev="$(git -C "$work" rev-parse HEAD)"
printf 'newer\n' >> "$work/server/value.txt"
git -C "$work" add .
git -C "$work" commit -m newer >/dev/null
newestrev="$(git -C "$work" rev-parse HEAD)"
zero_sha='0000000000000000000000000000000000000000'
git -C "$work" remote add bare "$bare"
git -C "$work" push bare "$newrev:refs/heads/master" >/dev/null
git -C "$work" push bare "$newestrev:refs/heads/fixture-newest" >/dev/null

printf 'contract-user\n' > "$secrets/zdzq-hook-user"
printf 'ContractToken-NotForProduction\n' > "$secrets/zdzq-hook-token"

hook="$test_root/post-receive"
sed \
  -e "s|/volume1/docker/weavepress-git/.enable-auto-deploy|$enable|g" \
  -e "s|/volume1/docker/weavepress-git/.secrets|$secrets|g" \
  -e "s|/volume1/docker/weavepress-git/post-receive.log|$hook_log|g" \
  -e "s|/volume1/docker/weavepress-git/post-receive.lock|$lock_file|g" \
  "$source_hook" > "$hook"
chmod +x "$hook"

cat > "$mock_bin/curl" <<'MOCK_CURL'
#!/usr/bin/env bash
set -Eeuo pipefail
count_file="$MOCK_STATE/curl-count"
count=0
[[ ! -f "$count_file" ]] || count="$(<"$count_file")"
count=$((count + 1))
printf '%s' "$count" > "$count_file"
printf '%s\0' "$@" > "$MOCK_STATE/curl-argv-$count"
config=""
headers=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) config="${2:-}"; shift 2 ;;
    --dump-header) headers="${2:-}"; shift 2 ;;
    *) shift ;;
  esac
done
[[ -n "$config" && -f "$config" ]] || exit 90
command cp "$config" "$MOCK_STATE/curl-config-$count"
[[ -n "$headers" ]] || exit 91
if [[ "${MOCK_CURL_EXIT:-0}" -ne 0 ]]; then
  exit "$MOCK_CURL_EXIT"
fi
printf 'HTTP/1.1 %s Contract\r\n' "${MOCK_HTTP_CODE:-201}" > "$headers"
if [[ "${MOCK_LOCATION+x}" == x ]]; then
  printf 'Location: %s\r\n' "$MOCK_LOCATION" >> "$headers"
fi
printf '\r\n' >> "$headers"
printf '%s' "${MOCK_HTTP_CODE:-201}"
MOCK_CURL
chmod +x "$mock_bin/curl"

cat > "$mock_bin/flock" <<'MOCK_FLOCK'
#!/usr/bin/env bash
set -Eeuo pipefail
[[ "${1:-}" == -w && "${2:-}" == 30 && "${3:-}" == 9 ]] || exit 92
MOCK_FLOCK
chmod +x "$mock_bin/flock"

reset_case() {
  rm -f -- "$state"/* "$hook_log" "$lock_file"
}

run_hook() {
  local receive_record="$1"
  shift
  printf '%s\n' "$receive_record" | env \
    PATH="$mock_bin:$PATH" \
    MOCK_STATE="$state" \
    TMPDIR="$test_root" \
    GIT_DIR="$bare" \
    "$@" "$hook"
}

touch "$enable"
reset_case
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/123/ >/dev/null 2>&1 || \
  fail 'valid 201 responses with a relative Jenkins queue Location were rejected'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'server and gateway changes did not dispatch exactly two jobs'
for argv in "$state"/curl-argv-*; do
  if grep -aFq 'ContractToken-NotForProduction' "$argv"; then
    fail 'Jenkins token appeared in curl argv'
  fi
done
for config in "$state"/curl-config-*; do
  [[ "$(stat -c '%a' "$config")" == 600 ]] || fail 'copied curl config did not originate with mode 0600'
  grep -Fq 'ContractToken-NotForProduction' "$config" || fail 'curl config did not carry Jenkins authentication'
done
if find "$test_root" -maxdepth 1 -name 'weavepress-hook-curl.*' -print -quit | grep -q .; then
  fail 'hook left a curl credential or header file behind'
fi

reset_case
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=http://127.0.0.1:18080/queue/item/456/ >/dev/null 2>&1 || \
  fail 'valid 201 responses with an absolute same-origin Jenkins queue Location were rejected'

reset_case
run_hook "$zero_sha $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/457/ >/dev/null 2>&1 || \
  fail 'initial master creation did not enumerate and dispatch deployable components'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'initial master creation did not dispatch both changed components'

reset_case
run_hook "$oldrev $newrev refs/heads/ignored"$'\n'"$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/458/ >/dev/null 2>&1 || \
  fail 'multi-ref receive with one master record was rejected'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'multi-ref receive dispatched duplicate or missing jobs'

reset_case
if run_hook "$newrev $zero_sha refs/heads/master" env >/dev/null 2>&1; then
  fail 'master deletion was accepted as deployable'
fi
[[ ! -e "$state/curl-count" ]] || fail 'master deletion dispatched a Jenkins job'

for fixture in \
  'redirect|302|/login|0' \
  'wrong-status|200|/queue/item/1/|0' \
  'missing-location|201||0' \
  'external-location|201|http://example.invalid/queue/item/1/|0' \
  'timeout|201|/queue/item/1/|28'
do
  IFS='|' read -r name code location curl_exit <<< "$fixture"
  reset_case
  if [[ "$name" == missing-location ]]; then
    if run_hook "$oldrev $newrev refs/heads/master" env MOCK_HTTP_CODE="$code" MOCK_CURL_EXIT="$curl_exit" >/dev/null 2>&1; then
      fail "hook accepted $name Jenkins response"
    fi
  elif run_hook "$oldrev $newrev refs/heads/master" env MOCK_HTTP_CODE="$code" MOCK_LOCATION="$location" MOCK_CURL_EXIT="$curl_exit" >/dev/null 2>&1; then
    fail "hook accepted $name Jenkins response"
  fi
done

reset_case
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/789/ >/dev/null 2>&1 || \
  fail 'baseline dispatch failed before stale receive check'
git --git-dir "$bare" update-ref refs/heads/master "$newestrev"
reset_case
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/789/ >/dev/null 2>&1 || \
  fail 'stale receive was not skipped cleanly'
[[ ! -e "$state/curl-count" ]] || fail 'stale receive dispatched a Jenkins job'
grep -Fq 'stale' "$hook_log" || fail 'stale receive skip was not recorded'

reset_case
rm -f -- "$enable"
run_hook "$newrev $newestrev refs/heads/master" env >/dev/null 2>&1 || \
  fail 'disabled hook rejected a mirrored master update'
[[ ! -e "$state/curl-count" ]] || fail 'default-off hook dispatched a Jenkins job'

printf 'NAS hook behavior tests passed.\n'
