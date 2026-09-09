#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gateway="$repo_root/deploy/nginx.conf"
dev_compose_file="$repo_root/deploy/compose.yaml"
compose_file="$repo_root/deploy/production/compose.yaml"
compose_contract="$repo_root/deploy/tests/production-compose-contract.py"
ci_cd_contract="$repo_root/deploy/tests/ci-cd-contract.py"
production_script_dir="$repo_root/deploy/production/scripts"
production_scripts_behavior="$repo_root/deploy/tests/production-scripts-contract.sh"
python_bin="${PYTHON_BIN:-python3}"
contract_temp_dir="$(mktemp -d)"
trap 'rm -rf "$contract_temp_dir"' EXIT

extract_location_block() {
  local location_header="$1"
  local source_file="$2"
  local output_file="$3"

  awk -v expected="$location_header {" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }

    !capturing && trim($0) == expected {
      capturing = 1
    }

    capturing {
      print
      opening_line = $0
      closing_line = $0
      depth += gsub(/\{/, "", opening_line)
      depth -= gsub(/\}/, "", closing_line)
      if (depth == 0) {
        found = 1
        exit
      }
    }

    END {
      if (!found) {
        exit 1
      }
    }
  ' "$source_file" > "$output_file"
}

extract_server_level_directives() {
  local source_file="$1"
  local output_file="$2"

  awk '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }

    !capturing && trim($0) == "server {" {
      capturing = 1
      depth = 1
      next
    }

    capturing {
      if (depth == 1) {
        print
      }
      opening_line = $0
      closing_line = $0
      depth += gsub(/\{/, "", opening_line)
      depth -= gsub(/\}/, "", closing_line)
      if (depth == 0) {
        found = 1
        exit
      }
    }

    END {
      if (!found) {
        exit 1
      }
    }
  ' "$source_file" > "$output_file"
}

require_directive_in_block() {
  local block_file="$1"
  local directive_pattern="$2"
  local expected="$3"
  local location_header="$4"

  if ! grep -Eq "^[[:space:]]*${directive_pattern}[[:space:]]*(#.*)?$" "$block_file"; then
    printf '%s must contain: %s\n' "$location_header" "$expected" >&2
    return 1
  fi
}

require_only_directive_in_block() {
  local block_file="$1"
  local selector_pattern="$2"
  local directive_pattern="$3"
  local expected="$4"
  local context="$5"
  local active_count

  require_directive_in_block \
    "$block_file" "$directive_pattern" "$expected" "$context" || return 1
  active_count="$(grep -Ec "^[[:space:]]*${selector_pattern}" "$block_file" || true)"
  if [[ "$active_count" -ne 1 ]]; then
    printf '%s must contain exactly one active directive: %s\n' \
      "$context" "$expected" >&2
    return 1
  fi
}

reject_directive_in_block() {
  local block_file="$1"
  local directive_name="$2"
  local location_header="$3"

  if grep -Eq "^[[:space:]]*${directive_name}[[:space:]]+" "$block_file"; then
    printf '%s must not contain an active %s directive.\n' "$location_header" "$directive_name" >&2
    return 1
  fi
}

reject_pattern_in_block() {
  local block_file="$1"
  local directive_pattern="$2"
  local rejected="$3"
  local context="$4"

  if grep -Eq "^[[:space:]]*${directive_pattern}[[:space:]]*(#.*)?$" "$block_file"; then
    printf '%s must not contain: %s\n' "$context" "$rejected" >&2
    return 1
  fi
}

assert_proxy_location() {
  local gateway_file="$1"
  local location_header="$2"
  local block_name="$3"
  local block_file="$contract_temp_dir/$block_name.location"

  if ! extract_location_block "$location_header" "$gateway_file" "$block_file"; then
    printf 'Missing Nginx location block: %s\n' "$location_header" >&2
    return 1
  fi
  require_directive_in_block "$block_file" \
    'proxy_pass[[:space:]]+\$weavepress_api;' \
    'proxy_pass $weavepress_api;' "$location_header" || return 1
  reject_pattern_in_block "$block_file" \
    'proxy_pass[[:space:]]+http://weavepress-api:8080;' \
    'proxy_pass http://weavepress-api:8080;' "$location_header" || return 1
  require_directive_in_block "$block_file" \
    'proxy_set_header[[:space:]]+X-Forwarded-Proto[[:space:]]+\$http_x_forwarded_proto;' \
    'proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;' "$location_header" || return 1
}

assert_metrics_location() {
  local gateway_file="$1"
  local location_header='location = /metrics'
  local block_file="$contract_temp_dir/metrics.location"

  if ! extract_location_block "$location_header" "$gateway_file" "$block_file"; then
    printf 'Missing Nginx location block: %s\n' "$location_header" >&2
    return 1
  fi
  require_directive_in_block "$block_file" \
    'access_log[[:space:]]+off;' 'access_log off;' "$location_header" || return 1
  require_directive_in_block "$block_file" \
    'return[[:space:]]+404;' 'return 404;' "$location_header" || return 1
  reject_directive_in_block "$block_file" proxy_pass "$location_header" || return 1
}

assert_gateway_contract() {
  local gateway_file="$1"
  local server_directives="$contract_temp_dir/server.directives"

  if ! extract_server_level_directives "$gateway_file" "$server_directives"; then
    printf 'Missing Nginx server block.\n' >&2
    return 1
  fi
  require_only_directive_in_block "$server_directives" \
    'resolver[[:space:]]+' \
    'resolver[[:space:]]+127\.0\.0\.11[[:space:]]+valid=10s[[:space:]]+ipv6=off;' \
    'resolver 127.0.0.11 valid=10s ipv6=off;' 'server' || return 1
  require_only_directive_in_block "$server_directives" \
    'set[[:space:]]+\$weavepress_api[[:space:]]+' \
    'set[[:space:]]+\$weavepress_api[[:space:]]+http://weavepress-api:8080;' \
    'set $weavepress_api http://weavepress-api:8080;' 'server' || return 1
  require_only_directive_in_block "$server_directives" \
    'add_header[[:space:]]+X-WeavePress-Release[[:space:]]+' \
    'add_header[[:space:]]+X-WeavePress-Release[[:space:]]+__WEAVEPRESS_APP_SHA__[[:space:]]+always;' \
    'add_header X-WeavePress-Release __WEAVEPRESS_APP_SHA__ always;' 'server' || return 1

  assert_proxy_location "$gateway_file" 'location /api/' api || return 1
  assert_proxy_location "$gateway_file" 'location /media/' media || return 1
  assert_proxy_location "$gateway_file" 'location = /health' health || return 1
  assert_proxy_location "$gateway_file" 'location = /ready' ready || return 1
  assert_metrics_location "$gateway_file" || return 1
}

mutate_location_literal() {
  local source_file="$1"
  local location_header="$2"
  local original="$3"
  local replacement="$4"
  local output_file="$5"

  awk -v expected="$location_header {" -v original="$original" -v replacement="$replacement" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }

    !capturing && trim($0) == expected {
      capturing = 1
      found = 1
    }

    capturing && !changed && index($0, original) {
      position = index($0, original)
      $0 = substr($0, 1, position - 1) replacement substr($0, position + length(original))
      changed = 1
    }

    {
      print
      if (capturing) {
        opening_line = $0
        closing_line = $0
        depth += gsub(/\{/, "", opening_line)
        depth -= gsub(/\}/, "", closing_line)
        if (depth == 0) {
          capturing = 0
        }
      }
    }

    END {
      if (!found || !changed) {
        exit 1
      }
    }
  ' "$source_file" > "$output_file"
}

expect_gateway_rejection() {
  local mutation_name="$1"
  local mutated_gateway="$2"

  if assert_gateway_contract "$mutated_gateway" >/dev/null 2>&1; then
    printf 'Gateway contract accepted mutation: %s\n' "$mutation_name" >&2
    return 1
  fi
  printf 'Mutation rejected: %s\n' "$mutation_name"
}

run_gateway_self_test() {
  local health_mutant="$contract_temp_dir/health-upstream.conf"
  local metrics_mutant="$contract_temp_dir/metrics-proxy.conf"
  local proto_mutant="$contract_temp_dir/forwarded-proto.conf"
  local generic_upstream_mutant="$contract_temp_dir/generic-upstream.conf"
  local missing_resolver_mutant="$contract_temp_dir/missing-resolver.conf"
  local static_upstream_mutant="$contract_temp_dir/static-upstream.conf"
  local wrong_alias_mutant="$contract_temp_dir/wrong-alias.conf"
  local missing_release_header_mutant="$contract_temp_dir/missing-release-header.conf"

  assert_gateway_contract "$gateway"

  mutate_location_literal "$gateway" 'location = /health' \
    'proxy_pass $weavepress_api;' \
    $'# proxy_pass $weavepress_api;\n        proxy_pass http://wrong-api:8080;' "$health_mutant"
  mutate_location_literal "$gateway" 'location = /metrics' \
    'return 404;' $'# return 404;\n        proxy_pass http://weavepress-api:8080;' "$metrics_mutant"
  mutate_location_literal "$gateway" 'location /api/' \
    'proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;' \
    $'# proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;\n        proxy_set_header X-Forwarded-Proto $scheme;' "$proto_mutant"
  mutate_location_literal "$gateway" 'location /api/' \
    'proxy_pass $weavepress_api;' \
    'proxy_pass http://api:8080;' "$generic_upstream_mutant"
  mutate_location_literal "$gateway" 'server' \
    'resolver 127.0.0.11 valid=10s ipv6=off;' \
    '# resolver 127.0.0.11 valid=10s ipv6=off;' "$missing_resolver_mutant"
  mutate_location_literal "$gateway" 'location /api/' \
    'proxy_pass $weavepress_api;' \
    $'proxy_pass $weavepress_api;\n        proxy_pass http://weavepress-api:8080;' "$static_upstream_mutant"
  mutate_location_literal "$gateway" 'server' \
    'set $weavepress_api http://weavepress-api:8080;' \
    $'set $weavepress_api http://weavepress-api:8080;\n    set $weavepress_api http://api:8080;' "$wrong_alias_mutant"
  mutate_location_literal "$gateway" 'server' \
    'add_header X-WeavePress-Release __WEAVEPRESS_APP_SHA__ always;' \
    '# add_header X-WeavePress-Release __WEAVEPRESS_APP_SHA__ always;' "$missing_release_header_mutant"

  expect_gateway_rejection 'health proxy commented and upstream changed' "$health_mutant"
  expect_gateway_rejection 'metrics return commented and endpoint proxied' "$metrics_mutant"
  expect_gateway_rejection 'Proto header commented and changed to $scheme' "$proto_mutant"
  expect_gateway_rejection 'unique upstream changed to generic service alias' "$generic_upstream_mutant"
  expect_gateway_rejection 'Docker DNS resolver removed' "$missing_resolver_mutant"
  expect_gateway_rejection 'variable upstream changed to static upstream' "$static_upstream_mutant"
  expect_gateway_rejection 'upstream variable changed to generic service alias' "$wrong_alias_mutant"
  expect_gateway_rejection 'release header removed' "$missing_release_header_mutant"
}

require_script_literal() {
  local script_file="$1"
  local literal="$2"

  if ! grep -Fq -- "$literal" "$script_file"; then
    printf '%s must contain: %s\n' "$script_file" "$literal" >&2
    return 1
  fi
}

assert_before() {
  local script_file="$1"
  local first="$2"
  local second="$3"
  local first_line second_line

  first_line="$(grep -nF -- "$first" "$script_file" | head -n 1 | cut -d: -f1)"
  second_line="$(grep -nF -- "$second" "$script_file" | head -n 1 | cut -d: -f1)"
  if [[ -z "$first_line" || -z "$second_line" || "$first_line" -ge "$second_line" ]]; then
    printf '%s must place %s before %s\n' "$script_file" "$first" "$second" >&2
    return 1
  fi
}

assert_last_before() {
  local script_file="$1"
  local first="$2"
  local second="$3"
  local first_line second_line

  first_line="$(grep -nF -- "$first" "$script_file" | tail -n 1 | cut -d: -f1)"
  second_line="$(grep -nF -- "$second" "$script_file" | tail -n 1 | cut -d: -f1)"
  if [[ -z "$first_line" || -z "$second_line" || "$first_line" -ge "$second_line" ]]; then
    printf '%s must place the final %s before the final %s\n' "$script_file" "$first" "$second" >&2
    return 1
  fi
}

assert_control_character_matcher_semantics() {
  local i name value
  local samples=(
    "TAB" $'credential\tvalue'
    "ESC" $'credential\033value'
    "DEL" $'credential\177value'
  )

  for ((i = 0; i < ${#samples[@]}; i += 2)); do
    name="${samples[i]}"
    value="${samples[i + 1]}"
    if [[ "$value" == *$'\r'* || "$value" == *$'\n'* ]]; then
      printf 'Control-character fixture unexpectedly contains CR/LF: %s\n' "$name" >&2
      return 1
    fi
    printf 'Legacy CR/LF-only guard accepts control-character fixture: %s\n' "$name"
    if ! printf '%s' "$value" | LC_ALL=C grep -q '[[:cntrl:]]'; then
      printf 'POSIX control-character matcher missed fixture: %s\n' "$name" >&2
      return 1
    fi
  done
}

assert_redis_credential_case() {
  local case_name="$1"
  local expected="$2"
  local actual
  shift 2

  actual="$(env "$@" bash -u -c 'printf %s "${REDISCLI_AUTH:-${REDIS_PASSWORD:-}}"' 2>&1)" || {
    printf 'Redis credential case failed under nounset: %s\n' "$case_name" >&2
    return 1
  }
  [[ "$actual" == "$expected" ]] || {
    printf 'Redis credential case returned the wrong value: %s\n' "$case_name" >&2
    return 1
  }
}

assert_redis_credential_fallback_semantics() {
  local rediscli_fixture='rediscli-auth-fixture-secret'
  local legacy_fixture='redis-password-fixture-secret'
  local output status

  if output="$(
    {
      assert_redis_credential_case REDISCLI_AUTH-only "$rediscli_fixture" \
        -u REDIS_PASSWORD REDISCLI_AUTH="$rediscli_fixture" &&
      assert_redis_credential_case REDIS_PASSWORD-only "$legacy_fixture" \
        -u REDISCLI_AUTH REDIS_PASSWORD="$legacy_fixture" &&
      assert_redis_credential_case precedence "$rediscli_fixture" \
        REDISCLI_AUTH="$rediscli_fixture" REDIS_PASSWORD="$legacy_fixture" &&
      assert_redis_credential_case both-unset '' \
        -u REDISCLI_AUTH -u REDIS_PASSWORD &&
      printf 'Redis credential fallback behavior tests passed.\n'
    } 2>&1
  )"; then
    status=0
  else
    status=$?
  fi

  if [[ "$output" == *"$rediscli_fixture"* || "$output" == *"$legacy_fixture"* ]]; then
    printf 'Redis credential behavior test leaked a fixture secret.\n' >&2
    return 1
  fi
  if [[ "$status" -ne 0 ]]; then
    printf '%s\n' "$output" >&2
    return "$status"
  fi
  printf '%s\n' "$output"
}

assert_production_script_contract() {
  local script script_file
  local behavior_runner="$contract_temp_dir/production-scripts-behavior.function"
  local initialize="$production_script_dir/initialize-weavepress"
  local deploy="$production_script_dir/deploy-weavepress"
  local entrypoint="$production_script_dir/weavepress-deploy-entrypoint"
  local installer="$production_script_dir/weavepress-external-secrets-install"

  for script in initialize-weavepress deploy-weavepress weavepress-deploy-entrypoint weavepress-external-secrets-install; do
    script_file="$production_script_dir/$script"
    [[ -f "$script_file" ]] || {
      printf 'Missing production script: %s\n' "$script" >&2
      return 1
    }
    [[ -x "$script_file" ]] || {
      printf 'Production script is not executable: %s\n' "$script" >&2
      return 1
    }
    if LC_ALL=C grep -q $'\r' "$script_file"; then
      printf 'Production script must use LF endings: %s\n' "$script" >&2
      return 1
    fi
    bash -n "$script_file"
    if grep -Eq '^[[:space:]]*set[[:space:]]+-x' "$script_file"; then
      printf 'Production script must never enable shell tracing: %s\n' "$script" >&2
      return 1
    fi
  done

  [[ -x "$production_scripts_behavior" ]] || {
    printf 'Production script behavior test is not executable.\n' >&2
    return 1
  }
  if LC_ALL=C grep -q $'\r' "$production_scripts_behavior"; then
    printf 'Production script behavior test must use LF endings.\n' >&2
    return 1
  fi
  bash -n "$production_scripts_behavior"
  require_script_literal "$production_scripts_behavior" "command -v script >/dev/null 2>&1 || fail"
  if grep -Fq 'if command -v script' "$production_scripts_behavior"; then
    printf 'Production behavior tests must not silently skip TTY coverage.\n' >&2
    return 1
  fi

  declare -f run_production_scripts_behavior > "$behavior_runner"
  require_script_literal "$behavior_runner" '--pull never'
  require_script_literal "$behavior_runner" '--network none'
  require_script_literal "$behavior_runner" '-v "$docker_repo_root:/repo:ro"'
  if ! grep -Eq 'ubuntu@sha256:[0-9a-f]{64}' "$behavior_runner"; then
    printf 'Production behavior runner must use an immutable Ubuntu image digest.\n' >&2
    return 1
  fi
  if grep -Eq 'ubuntu:[0-9]' "$behavior_runner"; then
    printf 'Production behavior runner must not use a mutable Ubuntu tag.\n' >&2
    return 1
  fi

  require_script_literal "$initialize" 'set -Eeuo pipefail'
  require_script_literal "$initialize" 'umask 077'
  require_script_literal "$initialize" 'set +x'
  require_script_literal "$initialize" '[[ "${EUID:-$(id -u)}" -eq 0 ]] || die '\''must run as root'\'''
  require_script_literal "$initialize" 'DB_NAME="weavepress"'
  require_script_literal "$initialize" 'APP_DIR="${WEAVEPRESS_APP_DIR:-/opt/apps/weavepress}"'
  require_script_literal "$initialize" 'docker exec redis sh -c'
  require_script_literal "$initialize" 'REDISCLI_AUTH="${REDISCLI_AUTH:-${REDIS_PASSWORD:-}}" redis-cli'
  require_script_literal "$initialize" 'printf %s "${REDISCLI_AUTH:-${REDIS_PASSWORD:-}}"'
  require_script_literal "$initialize" '-n 7 DBSIZE'
  require_script_literal "$initialize" 'INFORMATION_SCHEMA.SCHEMATA'
  require_script_literal "$initialize" 'openssl rand -hex'
  require_script_literal "$initialize" 'APP_DB_USER="weavepress_app"'
  require_script_literal "$initialize" 'MIGRATOR_DB_USER="weavepress_migrator"'
  require_script_literal "$initialize" "FROM mysql.user WHERE User='weavepress_app' AND Host='%';"
  require_script_literal "$initialize" "FROM mysql.user WHERE User='weavepress_migrator' AND Host='%';"
  require_script_literal "$initialize" 'GRANT SELECT, INSERT, UPDATE, DELETE ON'
  require_script_literal "$initialize" 'CREATE, ALTER, INDEX, DROP, REFERENCES'
  require_script_literal "$initialize" 'chown 65532:65532'
  require_script_literal "$initialize" 'chmod 0600'
  require_script_literal "$initialize" 'created_schema=0'
  require_script_literal "$initialize" 'compensate_database_objects'
  require_script_literal "$initialize" 'ln "$env_stage" "$ENV_FILE"'
  require_script_literal "$initialize" '"$ENV_FILE" -ef "$env_stage"'
  require_script_literal "$initialize" 'config --quiet'
  if grep -Eq 'redis-cli([^[:alnum:]_-]|$).*((^|[[:space:]])-a|--pass)' "$initialize"; then
    printf '%s must use REDISCLI_AUTH instead of a Redis password argument.\n' "$initialize" >&2
    return 1
  fi
  assert_before "$initialize" 'Redis DB 7 is not empty' 'CREATE DATABASE'
  assert_before "$initialize" 'already exists in MySQL' 'CREATE DATABASE'
  assert_before "$initialize" 'config --quiet' 'CREATE DATABASE'
  assert_last_before "$initialize" 'ln "$env_stage" "$ENV_FILE"' 'initialization_complete=1'
  assert_last_before "$initialize" 'initialization_complete=1' 'if rm -f -- "$env_stage"'

  require_script_literal "$deploy" 'set +x'
  require_script_literal "$deploy" '[[ "${EUID:-$(id -u)}" -eq 0 ]] || die '\''must run as root'\'''
  require_script_literal "$deploy" 'flock -w 1800'
  require_script_literal "$deploy" 'WEAVEPRESS_LOCK_FILE:-/var/lock/weavepress-deploy.lock'
  require_script_literal "$deploy" '^[0-9a-f]{40}$'
  require_script_literal "$deploy" 'DOCKER_CONFIG'
  require_script_literal "$deploy" '--password-stdin'
  require_script_literal "$deploy" "LC_ALL=C grep -q '[[:cntrl:]]' < <(printf '%s'"
  require_script_literal "$deploy" 'read -r -t "$INPUT_TIMEOUT" -n'
  require_script_literal "$deploy" 'WEAVEPRESS_HEALTH_ATTEMPTS must be an integer from 1 to 120'
  require_script_literal "$deploy" 'WEAVEPRESS_HEALTH_DELAY must be an integer from 1 to 30'
  require_script_literal "$deploy" '--profile migrate run --rm migrate'
  require_script_literal "$deploy" '--no-deps api worker'
  require_script_literal "$deploy" '--no-deps gateway'
  require_script_literal "$deploy" '{{.State.Health.Status}}'
  require_script_literal "$deploy" '{{.State.Status}}'
  require_script_literal "$deploy" '{{.Config.Image}}'
  require_script_literal "$deploy" '{{.Image}}'
  require_script_literal "$deploy" '{{ index .Config.Labels "org.opencontainers.image.revision" }}'
  require_script_literal "$deploy" '{{range .RepoDigests}}{{println .}}{{end}}'
  require_script_literal "$deploy" 'docker pull "$api_digest_ref"'
  require_script_literal "$deploy" 'docker pull "$gateway_digest_ref"'
  require_script_literal "$deploy" 'docker image tag "$digest_ref" "$tag_ref"'
  require_script_literal "$deploy" 'API_IMAGE_DIGEST=%s'
  require_script_literal "$deploy" 'GATEWAY_IMAGE_DIGEST=%s'
  require_script_literal "$deploy" '/var/lib/zdzq-deploy/weavepress'
  require_script_literal "$deploy" 'rollback_tag='
  assert_before "$deploy" 'Gateway deployment requires a healthy weavepress-api' 'docker pull "$gateway_digest_ref"'

  require_script_literal "$entrypoint" 'SSH_ORIGINAL_COMMAND'
  require_script_literal "$entrypoint" '^[0-9a-f]{40}$'
  require_script_literal "$entrypoint" 'exec /usr/bin/sudo -n /usr/local/sbin/deploy-weavepress "$app_sha" "--component=$component"'

  require_script_literal "$installer" '[[ -t 0 && -t 1 ]]'
  require_script_literal "$installer" 'set +x'
  require_script_literal "$installer" '[[ "${EUID:-$(id -u)}" -eq 0 ]] || die '\''must run as root'\'''
  require_script_literal "$installer" 'read -rsp'
  require_script_literal "$installer" "LC_ALL=C grep -q '[[:cntrl:]]' < <(printf '%s'"
  require_script_literal "$installer" 'mktemp'
  require_script_literal "$installer" 'backup/env'
  require_script_literal "$installer" 'docker compose'
  require_script_literal "$installer" 'config --quiet'
  require_script_literal "$installer" 'WEAVEPRESS_LOCK_FILE:-/var/lock/weavepress-deploy.lock'
  require_script_literal "$installer" 'flock -w 1800'
  require_script_literal "$installer" 'if [[ "${BASH_SOURCE[0]}" == "$0" ]]'
  if grep -Eq '^[[:space:]]*(APP_SHA=.*[[:space:]])?docker compose .*(up|run|restart|start)' "$installer"; then
    printf 'External secrets installer must not start or restart services.\n' >&2
    return 1
  fi
  if grep -ERiq -- 'WEAVEPRESS_.*(ALLOW_UNPRIVILEGED|BYPASS_ROOT|TEST_ROOT)|SKIP_ROOT_CHECK' "$production_script_dir"; then
    printf 'Production scripts must not contain a test-only root bypass.\n' >&2
    return 1
  fi
}

expect_entrypoint_rejection() {
  local original_command="$1"
  local entrypoint="$production_script_dir/weavepress-deploy-entrypoint"

  if SSH_ORIGINAL_COMMAND="$original_command" "$entrypoint" </dev/null >/dev/null 2>&1; then
    printf 'Forced-command entrypoint accepted unsafe command: %q\n' "$original_command" >&2
    return 1
  fi
}

run_production_scripts_behavior() {
  local docker_repo_root="$repo_root"

  if [[ "$(id -u)" -eq 0 ]]; then
    bash "$production_scripts_behavior"
    return
  fi

  case "$(uname -s)" in
    CYGWIN*|MINGW*|MSYS*) docker_repo_root="$(cygpath -w "$repo_root")" ;;
  esac
  MSYS_NO_PATHCONV=1 docker run --rm --user 0:0 --pull never --network none \
    -v "$docker_repo_root:/repo:ro" \
    -w /repo \
    ubuntu@sha256:0e0a0fc6d18feda9db1590da249ac93e8d5abfea8f4c3c0c849ce512b5ef8982 \
    bash deploy/tests/production-scripts-contract.sh
}

run_post_receive_real_flock() {
  local docker_repo_root="$repo_root"

  case "$(uname -s)" in
    CYGWIN*|MINGW*|MSYS*) docker_repo_root="$(cygpath -w "$repo_root")" ;;
  esac
  MSYS_NO_PATHCONV=1 docker run --rm --user 0:0 --pull never --network none \
    -v "$docker_repo_root:/repo:ro" \
    -w /repo \
    golang@sha256:e401dae1bf814e29204a8cb7915682e1780951e609ca0dd8865ee1937f510c48 \
    bash deploy/tests/post-receive-contract.sh --require-real-flock
}

run_production_script_self_test() {
  assert_control_character_matcher_semantics
  assert_redis_credential_fallback_semantics
  assert_production_script_contract
  expect_entrypoint_rejection 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA --component=server'
  expect_entrypoint_rejection 'deploy-weavepress 0000000000000000000000000000000000000000 --component=server'
  expect_entrypoint_rejection '0000000000000000000000000000000000000000 --component=server extra'
  expect_entrypoint_rejection '0000000000000000000000000000000000000000 --component=server;id'
  expect_entrypoint_rejection $'0000000000000000000000000000000000000000 --component=server\nextra'
  run_production_scripts_behavior
  printf 'Production script safety self-tests passed.\n'
}

render_compose_model() {
  local output_file="$1"

  APP_SHA=0000000000000000000000000000000000000000 \
    docker compose --profile migrate \
    --env-file "$repo_root/deploy/production/.env.example" \
    -f "$compose_file" config --format json > "$output_file"
}

render_dev_compose_model() {
  local output_file="$1"

  docker compose -f "$dev_compose_file" config --format json > "$output_file"
}

case "${1:-}" in
  --gateway-only)
    if [[ $# -ne 2 ]]; then
      printf 'Usage: %s --gateway-only <nginx.conf>\n' "$0" >&2
      exit 2
    fi
    assert_gateway_contract "$2"
    ;;
  --gateway-self-test)
    if [[ $# -ne 1 ]]; then
      printf 'Usage: %s --gateway-self-test\n' "$0" >&2
      exit 2
    fi
    run_gateway_self_test
    ;;
  --scripts-self-test)
    if [[ $# -ne 1 ]]; then
      printf 'Usage: %s --scripts-self-test\n' "$0" >&2
      exit 2
    fi
    run_production_script_self_test
    ;;
  --self-test)
    if [[ $# -ne 1 ]]; then
      printf 'Usage: %s --self-test\n' "$0" >&2
      exit 2
    fi
    compose_model="$contract_temp_dir/compose.json"
    dev_compose_model="$contract_temp_dir/dev-compose.json"
    "$python_bin" -B "$ci_cd_contract"
    run_post_receive_real_flock
    run_gateway_self_test
    run_production_script_self_test
    "$python_bin" -B "$compose_contract" --source-self-test
    "$python_bin" -B "$compose_contract" --source "$compose_file"
    render_compose_model "$compose_model"
    "$python_bin" -B "$compose_contract" --self-test "$compose_model"
    render_dev_compose_model "$dev_compose_model"
    "$python_bin" -B "$compose_contract" --dev-self-test "$dev_compose_model"
    ;;
  '')
    "$python_bin" -B "$ci_cd_contract"
    run_post_receive_real_flock
    assert_gateway_contract "$gateway"
    run_production_script_self_test
    compose_model="$contract_temp_dir/compose.json"
    dev_compose_model="$contract_temp_dir/dev-compose.json"
    "$python_bin" -B "$compose_contract" --source "$compose_file"
    render_compose_model "$compose_model"
    "$python_bin" -B "$compose_contract" "$compose_model"
    render_dev_compose_model "$dev_compose_model"
    "$python_bin" -B "$compose_contract" --dev "$dev_compose_model"

    docker run --rm --pull never --network none --add-host weavepress-api:127.0.0.1 \
      -v "$gateway:/etc/nginx/conf.d/default.conf:ro" \
      nginx@sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10 nginx -t
    ;;
  *)
    printf 'Unknown option: %s\n' "$1" >&2
    exit 2
    ;;
esac
