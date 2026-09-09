#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
freshness="$repo_root/deploy/jenkins/assert-component-current.sh"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  printf 'Component freshness behavior violation: %s\n' "$1" >&2
  exit 1
}

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

printf 'Component freshness behavior tests passed.\n'
