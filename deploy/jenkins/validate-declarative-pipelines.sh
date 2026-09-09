#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
set +x

die() {
  printf 'validate-declarative-pipelines: %s\n' "$1" >&2
  exit 1
}

[[ $# -eq 3 ]] || die 'expected Jenkins URL, user file, and API token file'
jenkins_url="${1%/}"
user_file="$2"
token_file="$3"
[[ "$jenkins_url" =~ ^https?://[^/]+(:[0-9]+)?$ ]] || die 'Jenkins URL is invalid'
[[ -f "$user_file" && -r "$user_file" && ! -L "$user_file" ]] || die 'Jenkins user file is unsafe'
[[ -f "$token_file" && -r "$token_file" && ! -L "$token_file" ]] || die 'Jenkins API token file is unsafe'
IFS= read -r jenkins_user < "$user_file" || die 'could not read Jenkins user'
IFS= read -r jenkins_token < "$token_file" || die 'could not read Jenkins API token'
[[ -n "$jenkins_user" && -n "$jenkins_token" ]] || die 'Jenkins credentials are empty'

curl_config="$(mktemp)"
response="$(mktemp)"
cleanup() {
  jenkins_user=""
  jenkins_token=""
  rm -f -- "$curl_config" "$response"
}
trap cleanup EXIT
chmod 0600 "$curl_config" "$response"
printf 'user = "%s:%s"\n' "$jenkins_user" "$jenkins_token" > "$curl_config"
jenkins_user=""
jenkins_token=""

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
for pipeline in \
  WeavePressServer.groovy \
  WeavePressGateway.groovy \
  ServerSyncGitHubAndDeploy.groovy \
  GatewaySyncGitHubAndDeploy.groovy
do
  : > "$response"
  http_code="$(curl --config "$curl_config" --noproxy '*' --silent --show-error --max-redirs 0 \
    --connect-timeout 5 --max-time 30 --output "$response" --write-out '%{http_code}' \
    --form "jenkinsfile=<$script_dir/$pipeline;type=text/plain" \
    "$jenkins_url/pipeline-model-converter/validate")" || die "Jenkins linter request failed for ${pipeline}"
  [[ "$http_code" == 200 ]] || die "Jenkins linter returned HTTP ${http_code} for ${pipeline}"
  [[ "$(<"$response")" == 'Jenkinsfile successfully validated.' ]] || \
    die "Jenkins Declarative validation failed for ${pipeline}"
  printf 'Validated %s\n' "$pipeline"
done
