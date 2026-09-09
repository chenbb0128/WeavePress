#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

die() {
  printf 'assert-component-current: %s\n' "$1" >&2
  exit 1
}

[[ $# -eq 3 ]] || die 'expected repository, component, and APP_SHA'
repository="$1"
component="$2"
app_sha="$3"
[[ -d "$repository/.git" ]] || die 'repository checkout is missing'
[[ "$component" == server || "$component" == gateway ]] || die 'component must be server or gateway'
[[ "$app_sha" =~ ^[0-9a-f]{40}$ ]] || die 'APP_SHA must be a complete lowercase commit SHA'

git -C "$repository" fetch --no-tags origin \
  +refs/heads/master:refs/remotes/origin/master >/dev/null 2>&1 || \
  die 'could not refresh NAS master'
git -C "$repository" cat-file -e "${app_sha}^{commit}" 2>/dev/null || die 'APP_SHA is not a commit'
master_sha="$(git -C "$repository" rev-parse --verify refs/remotes/origin/master)" || \
  die 'could not resolve NAS master'
[[ "$master_sha" =~ ^[0-9a-f]{40}$ ]] || die 'NAS master SHA is invalid'
git -C "$repository" merge-base --is-ancestor "$app_sha" "$master_sha" || \
  die 'APP_SHA is not an ancestor of NAS master'

changed_paths="$(mktemp)" || die 'could not allocate component diff buffer'
cleanup() {
  rm -f -- "$changed_paths"
}
trap cleanup EXIT
git -C "$repository" diff --no-renames --name-only -z "$app_sha" "$master_sha" -- > "$changed_paths" || \
  die 'could not compare APP_SHA with NAS master'

component_changed=false
while IFS= read -r -d '' path; do
  case "$component:$path" in
    server:server/*|server:deploy/production/*|server:deploy/jenkins/assert-component-current.sh|server:deploy/jenkins/publish-immutable-image.sh)
      component_changed=true
      ;;
    gateway:admin/*|gateway:web/*|gateway:.dockerignore|gateway:deploy/Dockerfile.gateway|gateway:deploy/Dockerfile.gateway.dockerignore|gateway:deploy/nginx.conf|gateway:deploy/jenkins/assert-component-current.sh|gateway:deploy/jenkins/publish-immutable-image.sh|gateway:deploy/jenkins/verify-gateway-release.sh)
      component_changed=true
      ;;
  esac
done < "$changed_paths"

[[ "$component_changed" == false ]] || \
  die 'a newer NAS master commit changes the requested component'
