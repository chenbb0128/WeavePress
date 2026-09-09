#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

readonly POLICY_VERIFIER="/var/jenkins_home/weavepress/bin/verify-acr-immutable-policy"
readonly POLICY_TIMEOUT_SECONDS=30

die() {
  printf 'verify-acr-immutable-policy: %s\n' "$1" >&2
  exit 1
}

[[ $# -ge 1 ]] || die 'expected at least one ACR repository'
for repository in "$@"; do
  [[ "$repository" =~ ^registry\.cn-hangzhou\.aliyuncs\.com/zdzq/weavepress-(api|worker|migrate|gateway)$ ]] || \
    die 'repository is outside the WeavePress allowlist'
done

if [[ ! -e "$POLICY_VERIFIER" ]]; then
  [[ ! -L "$POLICY_VERIFIER" ]] || die 'trusted ACR immutable-policy verifier path is an unsafe symlink'
  printf '%s\n' \
    'WARNING: ACR immutable-tag policy verifier is not installed; continuing with Jenkins single-writer, remote content reconciliation, and production pull-by-digest controls.' >&2
  exit 0
fi
[[ -f "$POLICY_VERIFIER" && -x "$POLICY_VERIFIER" && ! -L "$POLICY_VERIFIER" ]] || \
  die 'trusted ACR immutable-policy verifier exists but is unsafe or not executable'

for repository in "$@"; do
  if result="$(timeout "$POLICY_TIMEOUT_SECONDS" "$POLICY_VERIFIER" "$repository")"; then
    :
  else
    die "could not verify immutable-tag policy for ${repository}"
  fi
  [[ "$result" == "repository=${repository} immutable=true" ]] || \
    die "immutable-tag policy is not positively verified for ${repository}"
done

printf 'ACR immutable-tag policy verified for %s repositories.\n' "$#"
