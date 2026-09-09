#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

require_real_flock=false
case "${1:-}" in
  '') ;;
  --require-real-flock) require_real_flock=true ;;
  *)
    printf 'Usage: %s [--require-real-flock]\n' "$0" >&2
    exit 2
    ;;
esac

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
real_flock="$(command -v flock || true)"
mkdir -p "$mock_bin" "$state" "$secrets"
git init --bare "$bare" >/dev/null
git init "$work" >/dev/null
git -C "$work" config user.name 'CI contract'
git -C "$work" config user.email 'ci-contract@example.invalid'
mkdir -p "$work/server" "$work/admin"
printf 'old\n' > "$work/server/value.txt"
printf 'old\n' > "$work/admin/value.txt"
git -C "$work" add .
git -C "$work" commit -m old >/dev/null
oldrev="$(git -C "$work" rev-parse HEAD)"
printf 'new\n' > "$work/admin/value.txt"
git -C "$work" add .
git -C "$work" commit -m gateway-only >/dev/null
newrev="$(git -C "$work" rev-parse HEAD)"
printf 'new\n' > "$work/server/value.txt"
git -C "$work" add .
git -C "$work" commit -m server-only >/dev/null
newestrev="$(git -C "$work" rev-parse HEAD)"
zero_sha='0000000000000000000000000000000000000000'
server_baseline_ref='refs/weavepress/last-processed-server'
gateway_baseline_ref='refs/weavepress/last-processed-gateway'
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
  -e 's|readonly TRIGGER_RETRY_DELAY_SECONDS=2|readonly TRIGGER_RETRY_DELAY_SECONDS=0|' \
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
if [[ "${MOCK_DELAY_CALL:-0}" -eq "$count" ]]; then
  : > "$MOCK_STATE/curl-call-$count-started"
  sleep "${MOCK_DELAY_SECONDS:-1}"
fi
if [[ "${MOCK_FAIL_CALL:-0}" -eq "$count" ]]; then
  exit 28
fi
case ",${MOCK_FAIL_CALLS:-}," in
  *",$count,"*) exit 28 ;;
esac
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
[[ "${1:-}" == -w && "${2:-}" =~ ^[0-9]+$ && "${3:-}" == 9 ]] || exit 92
[[ "${MOCK_FLOCK_MARK_ATTEMPT:-0}" != 1 ]] || : > "$MOCK_STATE/stale-flock-attempted"
[[ -z "${REAL_FLOCK:-}" ]] || exec "$REAL_FLOCK" "$@"
MOCK_FLOCK
chmod +x "$mock_bin/flock"

reset_case() {
  rm -f -- "$state"/* "$hook_log" "$lock_file"
}

prepare_case() {
  local live="$1"
  local server_baseline="${2:-}"
  local gateway_baseline="${3:-${2:-}}"
  git --git-dir "$bare" update-ref refs/heads/master "$live"
  if [[ -n "$server_baseline" ]]; then
    git --git-dir "$bare" update-ref "$server_baseline_ref" "$server_baseline"
  else
    git --git-dir "$bare" update-ref -d "$server_baseline_ref" >/dev/null 2>&1 || true
  fi
  if [[ -n "$gateway_baseline" ]]; then
    git --git-dir "$bare" update-ref "$gateway_baseline_ref" "$gateway_baseline"
  else
    git --git-dir "$bare" update-ref -d "$gateway_baseline_ref" >/dev/null 2>&1 || true
  fi
  reset_case
}

assert_baselines() {
  local expected_server="$1"
  local expected_gateway="$2"
  local actual_server actual_gateway
  actual_server="$(git --git-dir "$bare" rev-parse --verify "$server_baseline_ref")" || \
    fail 'persistent Server processing baseline is missing'
  actual_gateway="$(git --git-dir "$bare" rev-parse --verify "$gateway_baseline_ref")" || \
    fail 'persistent Gateway processing baseline is missing'
  [[ "$actual_server" == "$expected_server" ]] || \
    fail "Server processing baseline is $actual_server, expected $expected_server"
  [[ "$actual_gateway" == "$expected_gateway" ]] || \
    fail "Gateway processing baseline is $actual_gateway, expected $expected_gateway"
}

run_hook() {
  local receive_record="$1"
  shift
  printf '%s\n' "$receive_record" | env \
    PATH="$mock_bin:$PATH" \
    MOCK_STATE="$state" \
    REAL_FLOCK="$real_flock" \
    TMPDIR="$test_root" \
    GIT_DIR="$bare" \
    "$@" "$hook"
}

assert_invalid_credentials_rejected() {
  local case_name="$1"

  prepare_case "$newrev" "$oldrev"
  if run_hook "$oldrev $newrev refs/heads/master" env \
    MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/credential-bypass/ >/dev/null 2>&1; then
    fail "$case_name credential was accepted when Jenkins returned 201"
  fi
  [[ ! -e "$state/curl-count" ]] || fail "$case_name credential reached curl"
  assert_baselines "$newrev" "$oldrev"
  printf 'contract-user\n' > "$secrets/zdzq-hook-user"
  printf 'ContractToken-NotForProduction\n' > "$secrets/zdzq-hook-token"
}

touch "$enable"
prepare_case "$newrev" "$oldrev"
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/123/ >/dev/null 2>&1 || \
  fail 'valid 201 responses with a relative Jenkins queue Location were rejected'
[[ "$(<"$state/curl-count")" == 1 ]] || fail 'Gateway-only change did not dispatch exactly one job'
grep -aFq 'WeavePressGateway' "$state/curl-argv-1" || fail 'Gateway-only change dispatched the wrong component'
assert_baselines "$newrev" "$newrev"
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

rm -f -- "$secrets/zdzq-hook-user"
assert_invalid_credentials_rejected 'missing Jenkins user'
printf 'contract-user\nunexpected-second-line\n' > "$secrets/zdzq-hook-user"
assert_invalid_credentials_rejected 'multiline Jenkins user'
printf 'ContractToken-NotForProduction\001suffix\n' > "$secrets/zdzq-hook-token"
assert_invalid_credentials_rejected 'control-character Jenkins token'

prepare_case "$newrev" "$oldrev"
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=http://127.0.0.1:18080/queue/item/456/ >/dev/null 2>&1 || \
  fail 'valid 201 responses with an absolute same-origin Jenkins queue Location were rejected'

prepare_case "$newestrev"
run_hook "$zero_sha $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/457/ >/dev/null 2>&1 || \
  fail 'initial master creation did not enumerate and dispatch deployable components'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'initial master creation did not dispatch both changed components'
assert_baselines "$newestrev" "$newestrev"

prepare_case "$newrev" "$oldrev"
run_hook "$oldrev $newrev refs/heads/ignored"$'\n'"$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/458/ >/dev/null 2>&1 || \
  fail 'multi-ref receive with one master record was rejected'
[[ "$(<"$state/curl-count")" == 1 ]] || fail 'multi-ref receive dispatched duplicate or missing jobs'

prepare_case "$newrev" "$oldrev"
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
  prepare_case "$newrev" "$oldrev"
  if [[ "$name" == missing-location ]]; then
    if run_hook "$oldrev $newrev refs/heads/master" env MOCK_HTTP_CODE="$code" MOCK_CURL_EXIT="$curl_exit" >/dev/null 2>&1; then
      fail "hook accepted $name Jenkins response"
    fi
  elif run_hook "$oldrev $newrev refs/heads/master" env MOCK_HTTP_CODE="$code" MOCK_LOCATION="$location" MOCK_CURL_EXIT="$curl_exit" >/dev/null 2>&1; then
    fail "hook accepted $name Jenkins response"
  fi
done

prepare_case "$newrev" "$oldrev"
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/789/ >/dev/null 2>&1 || \
  fail 'normal-order Gateway receive failed'
[[ "$(<"$state/curl-count")" == 1 ]] || fail 'normal-order Gateway receive did not dispatch once'
grep -aFq "APP_SHA=$newrev" "$state/curl-argv-1" || fail 'normal-order Gateway dispatch used the wrong SHA'
git --git-dir "$bare" update-ref refs/heads/master "$newestrev"
run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/790/ >/dev/null 2>&1 || \
  fail 'normal-order Server receive failed'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'normal-order receives did not dispatch one job per component change'
grep -aFq 'WeavePressServer' "$state/curl-argv-2" || fail 'normal-order Server receive dispatched the wrong component'
grep -aFq "APP_SHA=$newestrev" "$state/curl-argv-2" || fail 'normal-order Server dispatch used the wrong SHA'
assert_baselines "$newestrev" "$newestrev"

prepare_case "$newestrev" "$oldrev"
run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/791/ >/dev/null 2>&1 || \
  fail 'latest Server receive failed when processed before stale Gateway receive'
[[ "$(<"$state/curl-count")" == 2 ]] || \
  fail 'latest-first processing did not accumulate Gateway and Server changes from the persistent baseline'
grep -aFq 'WeavePressServer' "$state/curl-argv-1" || fail 'latest-first processing missed Server dispatch'
grep -aFq 'WeavePressGateway' "$state/curl-argv-2" || fail 'latest-first processing missed Gateway dispatch'
grep -aFq "APP_SHA=$newestrev" "$state/curl-argv-1" || fail 'latest-first Server dispatch did not use live master'
grep -aFq "APP_SHA=$newestrev" "$state/curl-argv-2" || fail 'latest-first Gateway dispatch did not use live master'
assert_baselines "$newestrev" "$newestrev"
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/792/ >/dev/null 2>&1 || \
  fail 'stale Gateway receive was not handled cleanly after latest-first processing'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'stale receive dispatched after live master was fully processed'
grep -Fqi 'stale' "$hook_log" || fail 'stale receive handling was not recorded'

prepare_case "$newestrev" "$oldrev"
run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/793/ MOCK_FAIL_CALLS=2 >/dev/null 2>&1 || \
  fail 'Gateway first-attempt failure was not retried successfully'
[[ "$(<"$state/curl-count")" == 3 ]] || fail 'Gateway first-attempt failure did not produce exactly one retry'
grep -aFq 'WeavePressServer' "$state/curl-argv-1" || fail 'Server was not dispatched before Gateway retry'
grep -aFq 'WeavePressGateway' "$state/curl-argv-2" || fail 'Gateway first attempt was not dispatched second'
grep -aFq 'WeavePressGateway' "$state/curl-argv-3" || fail 'Gateway retry did not target only Gateway'
assert_baselines "$newestrev" "$newestrev"

prepare_case "$newestrev" "$oldrev"
run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/794/ MOCK_FAIL_CALLS=1 >/dev/null 2>&1 || \
  fail 'Server first-attempt failure was not retried successfully'
[[ "$(<"$state/curl-count")" == 3 ]] || fail 'Server first-attempt failure did not produce exactly one retry'
grep -aFq 'WeavePressServer' "$state/curl-argv-1" || fail 'Server first attempt targeted the wrong component'
grep -aFq 'WeavePressServer' "$state/curl-argv-2" || fail 'Server retry did not target only Server'
grep -aFq 'WeavePressGateway' "$state/curl-argv-3" || fail 'Gateway was not dispatched once after Server recovered'
assert_baselines "$newestrev" "$newestrev"

prepare_case "$newestrev" "$oldrev"
if run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/795/ MOCK_FAIL_CALLS=2,3,4 >/dev/null 2>&1; then
  fail 'persistent Gateway enqueue failure was accepted'
fi
[[ "$(<"$state/curl-count")" == 4 ]] || fail 'persistent Gateway failure did not exhaust three component attempts'
grep -aFq 'WeavePressServer' "$state/curl-argv-1" || fail 'Server did not succeed before persistent Gateway failure'
for call in 2 3 4; do
  grep -aFq 'WeavePressGateway' "$state/curl-argv-$call" || fail 'persistent Gateway retry targeted the wrong component'
done
assert_baselines "$newestrev" "$oldrev"
grep -Fq "PENDING: ${GATEWAY_JOB:-WeavePress/WeavePressGateway} at $newestrev after 3 failed attempts" "$hook_log" || \
  fail 'persistent Gateway failure did not write a non-sensitive pending log'
run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/796/ >/dev/null 2>&1 || \
  fail 'same-live retry did not recover persistent Gateway failure'
[[ "$(<"$state/curl-count")" == 5 ]] || fail 'same-live retry repeated an already successful Server enqueue'
grep -aFq 'WeavePressGateway' "$state/curl-argv-5" || fail 'same-live retry did not target only failed Gateway'
assert_baselines "$newestrev" "$newestrev"

prepare_case "$newestrev" "$oldrev"
if run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/797/ MOCK_FAIL_CALLS=1,2,3 >/dev/null 2>&1; then
  fail 'persistent Server enqueue failure was accepted'
fi
[[ "$(<"$state/curl-count")" == 4 ]] || fail 'persistent Server failure did not continue to one Gateway enqueue'
for call in 1 2 3; do
  grep -aFq 'WeavePressServer' "$state/curl-argv-$call" || fail 'persistent Server retry targeted the wrong component'
done
grep -aFq 'WeavePressGateway' "$state/curl-argv-4" || fail 'Gateway was skipped after persistent Server failure'
assert_baselines "$oldrev" "$newestrev"
run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/798/ >/dev/null 2>&1 || \
  fail 'same-live retry did not recover persistent Server failure'
[[ "$(<"$state/curl-count")" == 5 ]] || fail 'same-live retry repeated an already successful Gateway enqueue'
grep -aFq 'WeavePressServer' "$state/curl-argv-5" || fail 'same-live retry did not target only failed Server'
assert_baselines "$newestrev" "$newestrev"

if [[ -n "$real_flock" ]]; then
  prepare_case "$newestrev" "$oldrev"
  run_hook "$newrev $newestrev refs/heads/master" env \
    MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/799/ \
    MOCK_DELAY_CALL=4 MOCK_DELAY_SECONDS=2 MOCK_FAIL_CALLS=2,3,4 >/dev/null 2>&1 &
  latest_hook_pid=$!
  for _ in {1..100}; do
    [[ ! -f "$state/curl-call-4-started" ]] || break
    sleep 0.05
  done
  [[ -f "$state/curl-call-4-started" ]] || {
    wait "$latest_hook_pid" >/dev/null 2>&1 || true
    fail 'latest hook did not reach its delayed final Gateway retry'
  }
  run_hook "$oldrev $newrev refs/heads/master" env \
    MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/800/ MOCK_FLOCK_MARK_ATTEMPT=1 >/dev/null 2>&1 &
  stale_hook_pid=$!
  for _ in {1..100}; do
    [[ ! -f "$state/stale-flock-attempted" ]] || break
    sleep 0.05
  done
  [[ -f "$state/stale-flock-attempted" ]] || fail 'stale hook did not reach flock while the latest hook held it'
  sleep 0.2
  [[ "$(<"$state/curl-count")" == 4 ]] || \
    fail 'waiting stale hook entered curl before the latest hook released the lock'
  kill -0 "$latest_hook_pid" 2>/dev/null || fail 'latest hook exited before stale lock contention was observed'
  kill -0 "$stale_hook_pid" 2>/dev/null || fail 'waiting stale hook exited before lock release'
  if wait "$latest_hook_pid"; then
    fail 'latest hook unexpectedly accepted its delayed partial enqueue failure'
  fi
  wait "$stale_hook_pid" || fail 'waiting stale hook did not recover after the latest hook failed'
  [[ "$(<"$state/curl-count")" == 5 ]] || \
    fail 'waiting stale hook repeated Server instead of retrying only failed Gateway'
  grep -aFq 'WeavePressGateway' "$state/curl-argv-5" || \
    fail 'waiting stale hook did not retry failed Gateway'
  assert_baselines "$newestrev" "$newestrev"
else
  [[ "$require_real_flock" == false ]] || fail 'real flock is required but unavailable'
  printf 'Real flock concurrency case skipped: flock is unavailable in this shell.\n'
fi

prepare_case "$newestrev" "$oldrev"
rm -f -- "$enable"
run_hook "$newrev $newestrev refs/heads/master" env >/dev/null 2>&1 || \
  fail 'disabled hook rejected a mirrored master update'
[[ ! -e "$state/curl-count" ]] || fail 'default-off hook dispatched a Jenkins job'
assert_baselines "$newestrev" "$newestrev"
touch "$enable"
run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/794/ >/dev/null 2>&1 || \
  fail 'enabling after a manual release baseline was recorded failed'
[[ ! -e "$state/curl-count" ]] || fail 'enabling after manual release redispatched already processed components'

printf 'node_modules\n' > "$work/.dockerignore"
git -C "$work" add .dockerignore
git -C "$work" commit -m gateway-root-ignore >/dev/null
root_ignore_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push bare master >/dev/null
prepare_case "$root_ignore_rev" "$newestrev"
run_hook "$newestrev $root_ignore_rev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/797/ >/dev/null 2>&1 || \
  fail 'root .dockerignore change was not processed'
[[ "$(<"$state/curl-count")" == 1 ]] || fail 'root .dockerignore change did not dispatch exactly one job'
grep -aFq 'WeavePressGateway' "$state/curl-argv-1" || fail 'root .dockerignore change did not dispatch Gateway'

mkdir -p "$work/deploy"
printf 'tmp\n' > "$work/deploy/Dockerfile.gateway.dockerignore"
git -C "$work" add deploy/Dockerfile.gateway.dockerignore
git -C "$work" commit -m gateway-dockerfile-ignore >/dev/null
dockerfile_ignore_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push bare master >/dev/null
prepare_case "$dockerfile_ignore_rev" "$root_ignore_rev"
run_hook "$root_ignore_rev $dockerfile_ignore_rev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/798/ >/dev/null 2>&1 || \
  fail 'Dockerfile-specific ignore change was not processed'
[[ "$(<"$state/curl-count")" == 1 ]] || fail 'Dockerfile-specific ignore change did not dispatch exactly one job'
grep -aFq 'WeavePressGateway' "$state/curl-argv-1" || fail 'Dockerfile-specific ignore change did not dispatch Gateway'

mkdir -p "$work/deploy/jenkins"
printf 'publisher\n' > "$work/deploy/jenkins/publish-immutable-image.sh"
git -C "$work" add deploy/jenkins/publish-immutable-image.sh
git -C "$work" commit -m publisher-safety >/dev/null
publisher_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push bare master >/dev/null
prepare_case "$publisher_rev" "$dockerfile_ignore_rev"
run_hook "$dockerfile_ignore_rev $publisher_rev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/799/ >/dev/null 2>&1 || \
  fail 'shared publisher change was not processed'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'shared publisher change did not dispatch both jobs'

printf 'NAS hook behavior tests passed.\n'
