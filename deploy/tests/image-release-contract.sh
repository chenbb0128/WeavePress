#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
publish="$repo_root/deploy/jenkins/publish-immutable-image.sh"
verify_gateway="$repo_root/deploy/jenkins/verify-gateway-release.sh"
test_root="$(mktemp -d)"
mock_bin="$test_root/bin"
state="$test_root/state"
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$mock_bin" "$state"

fail() {
  printf 'Image release behavior violation: %s\n' "$1" >&2
  exit 1
}

cat > "$mock_bin/docker" <<'MOCK_DOCKER'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'docker' >> "$MOCK_LOG"
printf ' %q' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
repository='registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api'
digest="${MOCK_REMOTE_DIGEST:-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
built_id='sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
remote_id="$built_id"
[[ "${MOCK_MANIFEST_MODE:-missing}" != different ]] || remote_id='sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'

if [[ "${1:-}" == manifest && "${2:-}" == inspect ]]; then
  case "${MOCK_MANIFEST_MODE:-missing}" in
    missing) printf 'manifest unknown\n' >&2; exit 1 ;;
    error) printf 'unauthorized\n' >&2; exit 1 ;;
    same|different) printf '{}\n'; exit 0 ;;
  esac
fi
if [[ "${1:-}" == pull ]]; then
  : > "$MOCK_STATE/pulled"
  exit 0
fi
if [[ "${1:-}" == push ]]; then
  : > "$MOCK_STATE/pushed"
  printf 'latest: digest: %s size: 1234\n' "$digest"
  exit 0
fi
if [[ "${1:-}" == image && "${2:-}" == inspect ]]; then
  format="${4:-}"
  if [[ "$format" == *'.Id'* ]]; then
    if [[ -f "$MOCK_STATE/pulled" ]]; then printf '%s\n' "$remote_id"; else printf '%s\n' "$built_id"; fi
  elif [[ "$format" == *org.opencontainers.image.revision* ]]; then
    printf '%s\n' "${MOCK_REMOTE_REVISION:-0123456789abcdef0123456789abcdef01234567}"
  elif [[ "$format" == *RepoDigests* ]]; then
    printf '%s@%s\n' "$repository" "$digest"
  fi
  exit 0
fi
exit 93
MOCK_DOCKER

cat > "$mock_bin/curl" <<'MOCK_CURL'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'curl' >> "$MOCK_LOG"
printf ' %q' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
headers=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dump-header) headers="${2:-}"; shift 2 ;;
    *) shift ;;
  esac
done
[[ -n "$headers" ]] || exit 94
printf 'HTTP/1.1 %s Contract\r\n' "${MOCK_HTTP_CODE:-200}" > "$headers"
if [[ "${MOCK_RELEASE_HEADER+x}" == x ]]; then
  printf 'X-WeavePress-Release: %s\r\n' "$MOCK_RELEASE_HEADER" >> "$headers"
fi
printf '\r\n' >> "$headers"
printf '%s' "${MOCK_HTTP_CODE:-200}"
MOCK_CURL
chmod +x "$mock_bin/docker" "$mock_bin/curl"

image='registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:0123456789abcdef0123456789abcdef01234567'
app_sha='0123456789abcdef0123456789abcdef01234567'
expected_digest='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'

run_publish() {
  local mode="$1"
  local output="$2"
  rm -rf -- "$state"
  mkdir -p "$state"
  : > "$test_root/mock.log"
  PATH="$mock_bin:$PATH" MOCK_LOG="$test_root/mock.log" MOCK_STATE="$state" MOCK_MANIFEST_MODE="$mode" \
    bash "$publish" "$image" "$app_sha" "$output"
}

output="$test_root/missing.digest"
run_publish missing "$output" >/dev/null 2>&1 || fail 'new immutable tag was rejected'
[[ "$(<"$output")" == "$expected_digest" ]] || fail 'new tag did not emit the pushed manifest digest'
[[ -e "$state/pushed" ]] || fail 'new immutable tag was not pushed'

output="$test_root/same.digest"
run_publish same "$output" >/dev/null 2>&1 || fail 'identical existing immutable tag was not reused'
[[ "$(<"$output")" == "$expected_digest" ]] || fail 'existing tag did not emit its manifest digest'
[[ ! -e "$state/pushed" ]] || fail 'identical existing immutable tag was pushed again'

for mode in different error; do
  output="$test_root/$mode.digest"
  if run_publish "$mode" "$output" >/dev/null 2>&1; then
    fail "unsafe existing-tag state was accepted: $mode"
  fi
  [[ ! -e "$state/pushed" ]] || fail "unsafe existing-tag state was pushed: $mode"
  [[ ! -e "$output" ]] || fail "unsafe existing-tag state wrote a digest: $mode"
done

: > "$test_root/mock.log"
PATH="$mock_bin:$PATH" MOCK_LOG="$test_root/mock.log" MOCK_HTTP_CODE=200 MOCK_RELEASE_HEADER="$app_sha" \
  bash "$verify_gateway" "$app_sha" https://wp.pdurl.cn/ready https://wp.pdurl.cn/ >/dev/null 2>&1 || \
  fail 'Gateway verifier rejected exact 200 responses with the requested release header'
[[ "$(grep -c '^curl ' "$test_root/mock.log")" == 2 ]] || fail 'Gateway verifier did not check both public endpoints'
grep -Fq -- '--max-redirs 0' "$test_root/mock.log" || fail 'Gateway verifier did not reject redirects'
if grep -Eq -- '(^| )--location( |$)' "$test_root/mock.log"; then fail 'Gateway verifier followed redirects'; fi

for fixture in 'redirect|302|' 'wrong-release|200|ffffffffffffffffffffffffffffffffffffffff' 'missing-release|200|'; do
  IFS='|' read -r name code release <<< "$fixture"
  : > "$test_root/mock.log"
  if [[ "$name" == missing-release ]]; then
    if PATH="$mock_bin:$PATH" MOCK_LOG="$test_root/mock.log" MOCK_HTTP_CODE="$code" \
      bash "$verify_gateway" "$app_sha" https://wp.pdurl.cn/ready >/dev/null 2>&1; then
      fail "Gateway verifier accepted $name"
    fi
  elif PATH="$mock_bin:$PATH" MOCK_LOG="$test_root/mock.log" MOCK_HTTP_CODE="$code" MOCK_RELEASE_HEADER="$release" \
    bash "$verify_gateway" "$app_sha" https://wp.pdurl.cn/ready >/dev/null 2>&1; then
    fail "Gateway verifier accepted $name"
  fi
done

printf 'Immutable image and Gateway release behavior tests passed.\n'
