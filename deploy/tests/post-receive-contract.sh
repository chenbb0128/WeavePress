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
source_dispatcher="$repo_root/deploy/nas/dispatch-pending"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'NAS hook behavior violation: %s\n' "$1" >&2
  if [[ -n "${hook_log:-}" && -f "$hook_log" ]]; then
    tail -n 20 "$hook_log" >&2
  fi
  exit 1
}

bare="$test_root/WeavePress.git"
work="$test_root/work"
mock_bin="$test_root/bin"
state="$test_root/state"
release_state="$test_root/release-state"
secrets="$test_root/secrets"
enable="$test_root/enable"
hook_log="$test_root/post-receive.log"
lock_file="$test_root/post-receive.lock"
real_flock="$(command -v flock || true)"
mkdir -p "$mock_bin" "$state" "$secrets" "$release_state"
chmod 0700 "$release_state"
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
server_baseline_file="$release_state/server.baseline"
gateway_baseline_file="$release_state/gateway.baseline"
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
  -e "s|/volume1/docker/weavepress-git/release-state|$release_state|g" \
  -e "s|/volume1/docker/weavepress-git/bin/dispatch-pending|$test_root/dispatch-pending|g" \
  -e 's|readonly TRIGGER_RETRY_DELAY_SECONDS=2|readonly TRIGGER_RETRY_DELAY_SECONDS=0|' \
  "$source_hook" > "$hook"
chmod +x "$hook"

dispatcher="$test_root/dispatch-pending"
sed \
  -e "s|readonly GIT_DIR=\"/volume1/docker/weavepress-git/WeavePress.git\"|readonly GIT_DIR=\"$bare\"|" \
  -e "s|readonly POST_RECEIVE=\"\\\$GIT_DIR/hooks/post-receive\"|readonly POST_RECEIVE=\"$hook\"|" \
  "$source_dispatcher" > "$dispatcher"
chmod +x "$dispatcher"

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
  exit "${MOCK_FAIL_EXIT:-28}"
fi
case ",${MOCK_FAIL_CALLS:-}," in
  *",$count,"*) exit "${MOCK_FAIL_EXIT:-28}" ;;
esac
if [[ "${MOCK_ACCEPT_THEN_DROP_CALL:-0}" -eq "$count" ]]; then
  : > "$MOCK_STATE/curl-call-$count-accepted"
  exit 52
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
if [[ "${1:-}" == -u && "${2:-}" == 9 ]]; then
  exit 0
fi
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
  git --git-dir "$bare" for-each-ref --format='delete %(refname)' refs/weavepress/ | \
    git --git-dir "$bare" update-ref --stdin >/dev/null
  rm -f -- "$release_state"/*
  if [[ -n "$server_baseline" ]]; then
    printf '%s\n' "$server_baseline" > "$server_baseline_file"
    chmod 0600 "$server_baseline_file"
  else
    rm -f -- "$server_baseline_file"
  fi
  if [[ -n "$gateway_baseline" ]]; then
    printf '%s\n' "$gateway_baseline" > "$gateway_baseline_file"
    chmod 0600 "$gateway_baseline_file"
  else
    rm -f -- "$gateway_baseline_file"
  fi
  reset_case
}

assert_baselines() {
  local expected_server="$1"
  local expected_gateway="$2"
  local actual_server actual_gateway
  [[ -f "$server_baseline_file" ]] || \
    fail 'persistent Server processing baseline is missing'
  [[ -f "$gateway_baseline_file" ]] || \
    fail 'persistent Gateway processing baseline is missing'
  actual_server="$(<"$server_baseline_file")"
  actual_gateway="$(<"$gateway_baseline_file")"
  [[ "$actual_server" == "$expected_server" ]] || \
    fail "Server processing baseline is $actual_server, expected $expected_server"
  [[ "$actual_gateway" == "$expected_gateway" ]] || \
    fail "Gateway processing baseline is $actual_gateway, expected $expected_gateway"
  [[ "$(stat -c '%a' "$server_baseline_file")" == 600 ]] || fail 'Server baseline mode is not 0600'
  [[ "$(stat -c '%a' "$gateway_baseline_file")" == 600 ]] || fail 'Gateway baseline mode is not 0600'
  [[ -z "$(git --git-dir "$bare" for-each-ref --format='%(refname)' refs/weavepress/)" ]] || \
    fail 'processing state remained client-pushable under refs/weavepress'
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

run_dispatcher() {
  local mode="$1"
  shift
  env \
    PATH="$mock_bin:$PATH" \
    MOCK_STATE="$state" \
    REAL_FLOCK="$real_flock" \
    TMPDIR="$test_root" \
    GIT_DIR="$bare" \
    "$@" "$dispatcher" "$mode"
}

assert_state_sha() {
  local file="$1"
  local expected="$2"
  [[ -f "$release_state/$file" ]] || fail "$file state is missing"
  [[ "$(<"$release_state/$file")" == "$expected" ]] || fail "$file state does not equal $expected"
  [[ "$(stat -c '%a' "$release_state/$file")" == 600 ]] || fail "$file mode is not 0600"
}

assert_state_absent() {
  [[ ! -e "$release_state/$1" ]] || fail "$1 state was not atomically acknowledged"
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
  assert_state_sha gateway.desired "$newrev"
  assert_state_absent gateway.uncertain
  printf 'contract-user\n' > "$secrets/zdzq-hook-user"
  printf 'ContractToken-NotForProduction\n' > "$secrets/zdzq-hook-token"
}

touch "$enable"
prepare_case "$newrev" "$oldrev"
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/123/ >/dev/null || \
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
  [[ "$(<"$state/curl-count")" == 1 ]] || fail "$name Jenkins result was blindly retried"
  assert_baselines "$newrev" "$oldrev"
  assert_state_sha gateway.uncertain "$newrev"
  assert_state_absent gateway.desired
  run_dispatcher --scheduled env MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/999/ >/dev/null 2>&1 || true
  [[ "$(<"$state/curl-count")" == 1 ]] || fail "$name uncertain dispatch was retried by the scheduler"
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
if run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/793/ MOCK_FAIL_CALLS=2 MOCK_FAIL_EXIT=7 >/dev/null 2>&1; then
  fail 'definite Gateway connection failure was accepted'
fi
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'Gateway connection failure was retried inside one dispatch pass'
assert_baselines "$newestrev" "$oldrev"
assert_state_sha gateway.desired "$newestrev"
assert_state_absent gateway.uncertain
run_dispatcher --scheduled env MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/794/ >/dev/null 2>&1 || \
  fail 'scheduled dispatcher did not recover Gateway without a new push'
[[ "$(<"$state/curl-count")" == 3 ]] || fail 'scheduled Gateway recovery repeated the successful Server enqueue'
grep -aFq 'WeavePressGateway' "$state/curl-argv-3" || fail 'scheduled recovery targeted the wrong component'
assert_baselines "$newestrev" "$newestrev"
assert_state_absent gateway.desired

prepare_case "$newestrev" "$oldrev"
if run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/795/ MOCK_FAIL_CALLS=1 MOCK_FAIL_EXIT=7 >/dev/null 2>&1; then
  fail 'definite Server connection failure was accepted'
fi
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'Server connection failure prevented one Gateway attempt or was retried inline'
assert_baselines "$oldrev" "$newestrev"
assert_state_sha server.desired "$newestrev"
run_dispatcher --scheduled env MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/796/ >/dev/null 2>&1 || \
  fail 'scheduled dispatcher did not recover Server without a new push'
[[ "$(<"$state/curl-count")" == 3 ]] || fail 'scheduled Server recovery repeated the successful Gateway enqueue'
grep -aFq 'WeavePressServer' "$state/curl-argv-3" || fail 'scheduled Server recovery targeted the wrong component'
assert_baselines "$newestrev" "$newestrev"

prepare_case "$newestrev" "$oldrev"
if run_hook "$newrev $newestrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/797/ MOCK_ACCEPT_THEN_DROP_CALL=2 >/dev/null 2>&1; then
  fail 'accepted-then-disconnected Gateway request was treated as definite failure'
fi
[[ -f "$state/curl-call-2-accepted" ]] || fail 'uncertain fixture did not record server-side acceptance'
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'uncertain Gateway request was blindly retried'
assert_baselines "$newestrev" "$oldrev"
assert_state_sha gateway.uncertain "$newestrev"
assert_state_absent gateway.desired
run_dispatcher --scheduled env MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/798/ >/dev/null 2>&1 || true
[[ "$(<"$state/curl-count")" == 2 ]] || fail 'scheduler retried an uncertain Gateway request'
grep -Fq "UNCERTAIN: WeavePress/WeavePressGateway at $newestrev" "$hook_log" || \
  fail 'uncertain Gateway request was not recorded without credentials'

prepare_case "$newrev" "$oldrev"
rm -f -- "$release_state/dispatcher.heartbeat"
run_hook "$oldrev $newrev refs/heads/master" env \
  MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/799/ >/dev/null 2>&1 || \
  fail 'direct hook required a periodic dispatcher heartbeat'
[[ "$(<"$state/curl-count")" == 1 ]] || fail 'direct hook did not enqueue exactly once without heartbeat'
assert_baselines "$newrev" "$newrev"

if [[ -n "$real_flock" ]]; then
  prepare_case "$newrev" "$oldrev"
  printf '%s\n' "$newrev" > "$release_state/gateway.desired"
  chmod 0600 "$release_state/gateway.desired"
  run_dispatcher --scheduled env MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/801/ \
    MOCK_DELAY_CALL=1 MOCK_DELAY_SECONDS=2 MOCK_FAIL_CALLS=1 MOCK_FAIL_EXIT=7 >/dev/null 2>&1 &
  latest_hook_pid=$!
  for _ in {1..100}; do
    [[ ! -f "$state/curl-call-1-started" ]] || break
    sleep 0.05
  done
  [[ -f "$state/curl-call-1-started" ]] || {
    wait "$latest_hook_pid" >/dev/null 2>&1 || true
    fail 'scheduled dispatcher did not reach its delayed Gateway attempt'
  }
  run_dispatcher --scheduled env \
    MOCK_HTTP_CODE=201 MOCK_LOCATION=/queue/item/802/ MOCK_FLOCK_MARK_ATTEMPT=1 >/dev/null 2>&1 &
  stale_hook_pid=$!
  for _ in {1..100}; do
    [[ ! -f "$state/stale-flock-attempted" ]] || break
    sleep 0.05
  done
  [[ -f "$state/stale-flock-attempted" ]] || fail 'stale hook did not reach flock while the latest hook held it'
  sleep 0.2
  [[ "$(<"$state/curl-count")" == 1 ]] || fail 'waiting dispatcher entered curl before lock release'
  kill -0 "$latest_hook_pid" 2>/dev/null || fail 'latest hook exited before stale lock contention was observed'
  kill -0 "$stale_hook_pid" 2>/dev/null || fail 'waiting stale hook exited before lock release'
  if wait "$latest_hook_pid"; then
    fail 'first dispatcher unexpectedly accepted its definite transport failure'
  fi
  wait "$stale_hook_pid" || fail 'waiting scheduled dispatcher did not recover after lock release'
  [[ "$(<"$state/curl-count")" == 2 ]] || fail 'lock-serialized recovery did not make exactly one later attempt'
  grep -aFq 'WeavePressGateway' "$state/curl-argv-2" || fail 'waiting dispatcher retried the wrong component'
  assert_baselines "$oldrev" "$newrev"
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
