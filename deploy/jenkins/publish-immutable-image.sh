#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
set +x

die() {
  printf 'publish-immutable-image: %s\n' "$1" >&2
  exit 1
}

[[ $# -eq 3 ]] || die 'expected image reference, APP_SHA, and digest output path'
image="$1"
app_sha="$2"
output="$3"
[[ "$app_sha" =~ ^[0-9a-f]{40}$ ]] || die 'APP_SHA must be a complete lowercase commit SHA'
[[ "$image" =~ ^registry\.cn-hangzhou\.aliyuncs\.com/zdzq/weavepress-(api|worker|migrate|gateway):${app_sha}$ ]] || \
  die 'image reference must be an allowed WeavePress repository tagged with APP_SHA'
[[ -n "$output" && ! -d "$output" ]] || die 'digest output path is invalid'

repository="${image%:*}"
manifest_error="$(mktemp)"
push_log="$(mktemp)"
output_temp=""
cleanup() {
  rm -f -- "$manifest_error" "$push_log"
  [[ -z "$output_temp" ]] || rm -f -- "$output_temp"
}
trap cleanup EXIT

inspect_id() {
  docker image inspect --format '{{.Id}}' "$1"
}

inspect_revision() {
  docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$1"
}

inspect_repository_digest() {
  local reference="$1"
  local candidate=""
  local found=""
  while IFS= read -r candidate; do
    [[ "$candidate" == "$repository"@sha256:* ]] || continue
    [[ "$candidate" =~ ^${repository}@sha256:[0-9a-f]{64}$ ]] || \
      die 'registry returned an invalid RepoDigest'
    [[ -z "$found" || "$found" == "$candidate" ]] || \
      die 'registry returned ambiguous RepoDigests'
    found="$candidate"
  done < <(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$reference")
  [[ -n "$found" ]] || die 'pulled image does not expose the expected RepoDigest'
  printf '%s' "${found#*@}"
}

verify_pulled_image() {
  local reference="$1"
  local expected_id="$2"
  local actual_id actual_revision
  actual_id="$(inspect_id "$reference")" || die 'could not inspect pulled image ID'
  [[ "$actual_id" =~ ^sha256:[0-9a-f]{64}$ ]] || die 'pulled image ID is invalid'
  [[ "$actual_id" == "$expected_id" ]] || die 'existing immutable tag has different image content'
  actual_revision="$(inspect_revision "$reference")" || die 'could not inspect pulled revision label'
  [[ "$actual_revision" == "$app_sha" ]] || die 'pulled image revision label does not match APP_SHA'
}

built_id="$(inspect_id "$image")" || die 'could not inspect built image ID'
[[ "$built_id" =~ ^sha256:[0-9a-f]{64}$ ]] || die 'built image ID is invalid'
built_revision="$(inspect_revision "$image")" || die 'could not inspect built revision label'
[[ "$built_revision" == "$app_sha" ]] || die 'built image revision label does not match APP_SHA'

remote_exists=0
if docker manifest inspect "$image" >/dev/null 2>"$manifest_error"; then
  remote_exists=1
elif ! grep -Eqi 'manifest unknown|no such manifest' "$manifest_error"; then
  die 'could not determine whether the immutable tag already exists'
fi

if [[ "$remote_exists" -eq 1 ]]; then
  docker pull "$image" >/dev/null 2>&1 || die 'could not pull the existing immutable tag'
  verify_pulled_image "$image" "$built_id"
  manifest_digest="$(inspect_repository_digest "$image")"
else
  timeout 900 docker push "$image" >"$push_log" 2>&1 || \
    die 'image push result is not confirmed; rerun only after immutable-tag reconciliation'
  manifest_digest="$(sed -nE 's/^.*digest: (sha256:[0-9a-f]{64}) size:.*$/\1/p' "$push_log" | tail -n 1)"
  [[ "$manifest_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || die 'docker push did not report a valid manifest digest'
  docker pull "$image" >/dev/null 2>&1 || die 'could not verify the pushed immutable tag'
  verify_pulled_image "$image" "$built_id"
  pulled_digest="$(inspect_repository_digest "$image")"
  [[ "$pulled_digest" == "$manifest_digest" ]] || \
    die 'immutable tag no longer resolves to the manifest digest just pushed'
fi

output_temp="$(mktemp "${output}.XXXXXX")" || die 'could not allocate digest output'
printf '%s\n' "$manifest_digest" > "$output_temp"
chmod 0600 "$output_temp"
mv -f -- "$output_temp" "$output"
output_temp=""
printf 'Confirmed immutable image %s.\n' "$image"
