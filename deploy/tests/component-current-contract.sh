#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
freshness="$repo_root/deploy/jenkins/assert-component-current.sh"
checkout_source="$repo_root/deploy/jenkins/checkout-component-source.sh"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'Component freshness behavior violation: %s\n' "$1" >&2
  exit 1
}

[[ -f "$checkout_source" ]] || fail 'checkout-component-source helper is missing'

bare="$test_root/origin.git"
work="$test_root/work"
checkout="$test_root/checkout"
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
printf 'gateway\n' > "$work/admin/value.txt"
git -C "$work" add .
git -C "$work" commit -m gateway-only >/dev/null
gatewayrev="$(git -C "$work" rev-parse HEAD)"
printf 'server\n' > "$work/server/value.txt"
git -C "$work" add .
git -C "$work" commit -m server-only >/dev/null
serverrev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" remote add origin "$bare"
git -C "$work" push origin master >/dev/null
git clone "$bare" "$checkout" >/dev/null
git -C "$checkout" checkout --detach "$gatewayrev" >/dev/null

bash "$freshness" "$checkout" gateway "$gatewayrev" >/dev/null || \
  fail 'Gateway ancestor was rejected after only a newer Server change'
if bash "$freshness" "$checkout" server "$gatewayrev" >/dev/null 2>&1; then
  fail 'Server ancestor was accepted despite a newer Server change'
fi
git --git-dir "$bare" update-ref refs/heads/master "$gatewayrev"
bash "$freshness" "$checkout" server "$oldrev" >/dev/null || \
  fail 'Server ancestor was rejected after only a newer Gateway change at the intermediate master'
if bash "$freshness" "$checkout" gateway "$oldrev" >/dev/null 2>&1; then
  fail 'Gateway ancestor was accepted despite a newer Gateway change'
fi
git --git-dir "$bare" update-ref refs/heads/master "$serverrev"

printf 'gateway-newer\n' > "$work/admin/value.txt"
git -C "$work" add .
git -C "$work" commit -m gateway-newer >/dev/null
newestrev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push origin master >/dev/null
if bash "$freshness" "$checkout" gateway "$gatewayrev" >/dev/null 2>&1; then
  fail 'older Gateway was accepted after a newer Gateway change'
fi
bash "$freshness" "$checkout" server "$serverrev" >/dev/null || \
  fail 'current Server component was rejected after only a newer Gateway change'
if bash "$freshness" "$checkout" gateway 0000000000000000000000000000000000000000 >/dev/null 2>&1; then
  fail 'invalid component SHA was accepted'
fi
[[ "$(git --git-dir "$bare" rev-parse refs/heads/master)" == "$newestrev" ]] || fail 'test fixture master did not advance'

printf 'node_modules\n' > "$work/.dockerignore"
git -C "$work" add .dockerignore
git -C "$work" commit -m gateway-root-ignore >/dev/null
root_ignore_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push origin master >/dev/null
if bash "$freshness" "$checkout" gateway "$newestrev" >/dev/null 2>&1; then
  fail 'Gateway ancestor was accepted after a newer root .dockerignore change'
fi
bash "$freshness" "$checkout" server "$newestrev" >/dev/null || \
  fail 'Server ancestor was rejected after only a newer root .dockerignore change'

mkdir -p "$work/deploy"
printf 'tmp\n' > "$work/deploy/Dockerfile.gateway.dockerignore"
git -C "$work" add deploy/Dockerfile.gateway.dockerignore
git -C "$work" commit -m gateway-dockerfile-ignore >/dev/null
dockerfile_ignore_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push origin master >/dev/null
if bash "$freshness" "$checkout" gateway "$root_ignore_rev" >/dev/null 2>&1; then
  fail 'Gateway ancestor was accepted after a newer Dockerfile-specific ignore change'
fi
bash "$freshness" "$checkout" server "$root_ignore_rev" >/dev/null || \
  fail 'Server ancestor was rejected after only a newer Dockerfile-specific ignore change'

mkdir -p "$work/deploy/jenkins"
printf 'publisher\n' > "$work/deploy/jenkins/publish-immutable-image.sh"
git -C "$work" add deploy/jenkins/publish-immutable-image.sh
git -C "$work" commit -m publisher-safety >/dev/null
publisher_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push origin master >/dev/null
if bash "$freshness" "$checkout" server "$dockerfile_ignore_rev" >/dev/null 2>&1; then
  fail 'Server ancestor was accepted after a newer shared publisher change'
fi
if bash "$freshness" "$checkout" gateway "$dockerfile_ignore_rev" >/dev/null 2>&1; then
  fail 'Gateway ancestor was accepted after a newer shared publisher change'
fi

printf 'verifier\n' > "$work/deploy/jenkins/verify-gateway-release.sh"
git -C "$work" add deploy/jenkins/verify-gateway-release.sh
git -C "$work" commit -m gateway-verifier-safety >/dev/null
verifier_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push origin master >/dev/null
bash "$freshness" "$checkout" server "$publisher_rev" >/dev/null || \
  fail 'Server ancestor was rejected after only a newer Gateway verifier change'
if bash "$freshness" "$checkout" gateway "$publisher_rev" >/dev/null 2>&1; then
  fail 'Gateway ancestor was accepted after a newer Gateway verifier change'
fi

printf 'freshness\n' > "$work/deploy/jenkins/assert-component-current.sh"
git -C "$work" add deploy/jenkins/assert-component-current.sh
git -C "$work" commit -m shared-freshness-safety >/dev/null
freshness_rev="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push origin master >/dev/null
if bash "$freshness" "$checkout" server "$verifier_rev" >/dev/null 2>&1; then
  fail 'Server ancestor was accepted after a newer shared freshness-helper change'
fi
if bash "$freshness" "$checkout" gateway "$verifier_rev" >/dev/null 2>&1; then
  fail 'Gateway ancestor was accepted after a newer shared freshness-helper change'
fi

git -C "$checkout" config user.name 'CI contract'
git -C "$checkout" config user.email 'ci-contract@example.invalid'
git -C "$checkout" checkout --detach "$oldrev" >/dev/null
printf 'divergent\n' > "$checkout/server/divergent.txt"
git -C "$checkout" add server/divergent.txt
git -C "$checkout" commit -m divergent >/dev/null
divergent_rev="$(git -C "$checkout" rev-parse HEAD)"
if bash "$freshness" "$checkout" server "$divergent_rev" >/dev/null 2>&1; then
  fail 'valid non-ancestor component SHA was accepted'
fi
[[ "$(git --git-dir "$bare" rev-parse refs/heads/master)" == "$freshness_rev" ]] || \
  fail 'test fixture master did not reach the final safety-helper commit'

bootstrap_bare="$test_root/bootstrap-origin.git"
bootstrap_work="$test_root/bootstrap-work"
bootstrap_checkout="$test_root/bootstrap-checkout"
bootstrap_runner="$test_root/checkout-component-source.sh"
bootstrap_current="$test_root/component-current.sh"
git init --bare "$bootstrap_bare" >/dev/null
git init "$bootstrap_work" >/dev/null
git -C "$bootstrap_work" config user.name 'CI contract'
git -C "$bootstrap_work" config user.email 'ci-contract@example.invalid'
mkdir -p "$bootstrap_work/server" "$bootstrap_work/deploy/jenkins"
printf 'server\n' > "$bootstrap_work/server/value.txt"
cp "$freshness" "$bootstrap_work/deploy/jenkins/assert-component-current.sh"
cp "$checkout_source" "$bootstrap_work/deploy/jenkins/checkout-component-source.sh"
git -C "$bootstrap_work" add .
git -C "$bootstrap_work" commit -m bootstrap >/dev/null
bootstrap_sha="$(git -C "$bootstrap_work" rev-parse HEAD)"
git -C "$bootstrap_work" remote add origin "$bootstrap_bare"
git -C "$bootstrap_work" push origin master >/dev/null
git clone --no-checkout --branch master --single-branch --no-tags \
  "$bootstrap_bare" "$bootstrap_checkout" >/dev/null
[[ ! -e "$bootstrap_checkout/deploy/jenkins/checkout-component-source.sh" ]] || \
  fail 'clone --no-checkout unexpectedly populated the helper in the worktree'
git -C "$bootstrap_checkout" show \
  'refs/remotes/origin/master:deploy/jenkins/checkout-component-source.sh' > "$bootstrap_runner"
chmod 0700 "$bootstrap_runner"
bash "$bootstrap_runner" "$bootstrap_checkout" server "$bootstrap_sha" "$bootstrap_current" >/dev/null || \
  fail 'origin/master bootstrap helper did not prepare the requested Server checkout'
[[ "$(git -C "$bootstrap_checkout" rev-parse HEAD)" == "$bootstrap_sha" ]] || \
  fail 'bootstrap helper did not detach at the requested APP_SHA'
tracked_mode="$(stat -c '%a' "$bootstrap_checkout/server/value.txt")" || \
  fail 'could not inspect bootstrap checkout file permissions'
(( (8#$tracked_mode & 0044) == 0044 )) || \
  fail "bootstrap helper checkout left tracked source unreadable by group/other (mode $tracked_mode)"
cmp -s "$freshness" "$bootstrap_current" || \
  fail 'bootstrap helper did not preserve the trusted origin/master freshness helper'

printf 'Component freshness behavior tests passed.\n'
