#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
initialize="$repo_root/deploy/production/scripts/initialize-weavepress"
deploy="$repo_root/deploy/production/scripts/deploy-weavepress"
external="$repo_root/deploy/production/scripts/weavepress-external-secrets-install"
test_root="$(mktemp -d)"
mock_bin="$test_root/bin"
mkdir -p "$mock_bin"
trap 'rm -rf "$test_root"' EXIT

fail() {
  printf 'Production script behavior violation: %s\n' "$1" >&2
  exit 1
}

write_mocks() {
  cat > "$mock_bin/docker" <<'MOCK_DOCKER'
#!/usr/bin/env bash
set -Eeuo pipefail

log_command() {
  {
    printf 'DOCKER_CONFIG=%q CMD=docker' "${DOCKER_CONFIG:-}"
    printf ' %q' "$@"
    printf '\n'
  } >> "$MOCK_LOG"
}

log_command "$@"
joined=" $* "

if [[ "${1:-}" == "login" ]]; then
  while IFS= read -r _; do :; done
  exit 0
fi

if [[ "${1:-}" == "exec" && "${2:-}" == "redis" ]]; then
  if [[ "$joined" == *" DBSIZE"* ]]; then
    printf '0\n'
  else
    printf '%s' "${MOCK_REDIS_PASSWORD:-RedisSafe_123}"
  fi
  exit 0
fi

if [[ "${1:-}" == "exec" && "${2:-}" == "mysql" ]]; then
  if [[ "$joined" == *"INFORMATION_SCHEMA.SCHEMATA"* ]]; then
    printf '%s\n' "${MOCK_SCHEMA_COUNT:-0}"
  elif [[ "$joined" == *"mysql.user"* && "$joined" == *"weavepress_app"* ]]; then
    printf '%s\n' "${MOCK_APP_USER_COUNT:-0}"
  elif [[ "$joined" == *"mysql.user"* && "$joined" == *"weavepress_migrator"* ]]; then
    printf '%s\n' "${MOCK_MIGRATOR_USER_COUNT:-0}"
  elif [[ "$joined" == *"INFORMATION_SCHEMA.TABLES"* ]]; then
    printf '0\n'
  else
    printf '%s\n' '-- mock dump'
  fi
  exit 0
fi

if [[ "${1:-}" == "exec" && "${2:-}" == "-i" && "${3:-}" == "mysql" ]]; then
  sql="$(command cat)"
  operation="UNKNOWN_SQL"
  case "$sql" in
    *"CREATE DATABASE"*) operation="CREATE_SCHEMA" ;;
    *"CREATE USER 'weavepress_app'"*) operation="CREATE_APP_USER" ;;
    *"GRANT"*"weavepress_app"*) operation="GRANT_APP" ;;
    *"CREATE USER 'weavepress_migrator'"*) operation="CREATE_MIGRATOR_USER" ;;
    *"GRANT"*"weavepress_migrator"*) operation="GRANT_MIGRATOR" ;;
    *"DROP USER"*"weavepress_migrator"*) operation="DROP_MIGRATOR_USER" ;;
    *"DROP USER"*"weavepress_app"*) operation="DROP_APP_USER" ;;
    *"DROP DATABASE"*) operation="DROP_SCHEMA" ;;
  esac
  printf 'SQL_OP=%s\n' "$operation" >> "$MOCK_LOG"
  [[ "${MOCK_FAIL_SQL_OP:-}" != "$operation" ]] || exit 1
  if [[ "${MOCK_CREATE_ENV_AFTER_SQL_OP:-}" == "$operation" ]]; then
    printf 'sentinel-existing-env\n' > "$WEAVEPRESS_APP_DIR/.env"
    chmod 0600 "$WEAVEPRESS_APP_DIR/.env"
  fi
  exit 0
fi

if [[ "${1:-}" == "compose" ]]; then
  if [[ "$joined" == *" config --quiet "* ]]; then
    if [[ "${MOCK_CREATE_ENV_DURING_CONFIG:-0}" == "1" ]]; then
      printf 'sentinel-existing-env\n' > "$WEAVEPRESS_APP_DIR/.env"
      chmod 0600 "$WEAVEPRESS_APP_DIR/.env"
    fi
    if [[ "${MOCK_COMPOSE_CONFIG_FAIL:-0}" == "1" ]]; then
      printf 'SENSITIVE_COMPOSE_DIAGNOSTIC\n' >&2
      exit 1
    fi
    exit 0
  fi

  if [[ "$joined" == *" up "* ]]; then
    mkdir -p "$MOCK_STATE_DIR"
    if [[ "$joined" == *" gateway "* ]]; then
      printf 'registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:%s\n' "$APP_SHA" > "$MOCK_STATE_DIR/gateway-image"
    fi
    if [[ "$joined" == *" api worker "* ]]; then
      printf 'registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:%s\n' "$APP_SHA" > "$MOCK_STATE_DIR/api-image"
      printf 'registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:%s\n' "$APP_SHA" > "$MOCK_STATE_DIR/worker-image"
    fi
    exit 0
  fi
  exit 0
fi

if [[ "${1:-}" == "inspect" ]]; then
  format="${3:-}"
  container="${4:-}"
  case "$format" in
    *State.Health.Status*)
      if [[ "$container" == "weavepress-api" && "${MOCK_FAIL_NEW_SERVER_HEALTH:-0}" == "1" && -f "$MOCK_STATE_DIR/api-image" ]] && \
        grep -Fq ':0123456789abcdef0123456789abcdef01234567' "$MOCK_STATE_DIR/api-image"; then
        printf 'unhealthy\n'
      else
        printf 'healthy\n'
      fi
      exit 0
      ;;
    *State.Status*) printf 'running\n'; exit 0 ;;
    *Config.Image*)
      case "$container" in
        weavepress-gateway)
          if [[ -f "$MOCK_STATE_DIR/gateway-image" ]]; then command cat "$MOCK_STATE_DIR/gateway-image"; else printf '%s\n' 'registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:old'; fi
          ;;
        weavepress-api)
          if [[ -f "$MOCK_STATE_DIR/api-image" ]]; then command cat "$MOCK_STATE_DIR/api-image"; else printf '%s\n' 'registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:old'; fi
          ;;
        weavepress-worker)
          if [[ -f "$MOCK_STATE_DIR/worker-image" ]]; then command cat "$MOCK_STATE_DIR/worker-image"; else printf '%s\n' 'registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:old'; fi
          ;;
      esac
      exit 0
      ;;
    *Image*)
      case "$container" in
        weavepress-gateway) [[ "${MOCK_GATEWAY_OLD:-0}" == "1" ]] || exit 1; printf 'sha256:old-gateway\n' ;;
        weavepress-api) [[ "${MOCK_SERVER_OLD:-0}" == "1" ]] || exit 1; printf 'sha256:old-api\n' ;;
        weavepress-worker) [[ "${MOCK_SERVER_OLD:-0}" == "1" ]] || exit 1; printf 'sha256:old-worker\n' ;;
      esac
      exit 0
      ;;
  esac
fi

exit 0
MOCK_DOCKER

  cat > "$mock_bin/openssl" <<'MOCK_OPENSSL'
#!/usr/bin/env bash
set -Eeuo pipefail
counter_file="$MOCK_STATE_DIR/openssl-counter"
mkdir -p "$MOCK_STATE_DIR"
count=0
[[ ! -f "$counter_file" ]] || count="$(<"$counter_file")"
count=$((count + 1))
printf '%s' "$count" > "$counter_file"
printf '%064d\n' "$count"
MOCK_OPENSSL

  cat > "$mock_bin/curl" <<'MOCK_CURL'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'CMD=curl' >> "$MOCK_LOG"
printf ' %q' "$@" >> "$MOCK_LOG"
printf '\n' >> "$MOCK_LOG"
counter_file="$MOCK_STATE_DIR/curl-counter"
count=0
[[ ! -f "$counter_file" ]] || count="$(<"$counter_file")"
count=$((count + 1))
printf '%s' "$count" > "$counter_file"
if (( count <= ${MOCK_CURL_FAILURES:-0} )); then
  exit 22
fi
MOCK_CURL

  cat > "$mock_bin/rm" <<'MOCK_RM'
#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${MOCK_FAIL_ENV_STAGE_REMOVE:-0}" == "1" && "$*" == *".env.initialize."* ]]; then
  marker="$MOCK_STATE_DIR/env-stage-remove-failed"
  if [[ ! -e "$marker" ]]; then
    : > "$marker"
    exit 1
  fi
fi
exec /usr/bin/rm "$@"
MOCK_RM

  cat > "$mock_bin/ln" <<'MOCK_LN'
#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${MOCK_SIGNAL_AFTER_ENV_LINK:-0}" == "1" && "${*: -1}" == "$WEAVEPRESS_APP_DIR/.env" ]]; then
  /usr/bin/ln "$@"
  kill -TERM "$PPID"
  exit 0
fi
exec /usr/bin/ln "$@"
MOCK_LN

  chmod +x "$mock_bin/docker" "$mock_bin/openssl" "$mock_bin/curl" "$mock_bin/rm" "$mock_bin/ln"
}

new_case() {
  local name="$1"
  CASE_DIR="$test_root/$name"
  APP_DIR="$CASE_DIR/app"
  STATE_DIR="$CASE_DIR/state"
  LOG_FILE="$CASE_DIR/mock.log"
  LOCK_FILE="$CASE_DIR/deploy.lock"
  METADATA_DIR="$CASE_DIR/metadata"
  mkdir -p "$APP_DIR" "$STATE_DIR" "$METADATA_DIR"
  : > "$LOG_FILE"
  cp "$repo_root/deploy/production/compose.yaml" "$APP_DIR/compose.yaml"
  cp "$repo_root/deploy/production/.env.example" "$APP_DIR/.env.example"
}

run_initialize() {
  PATH="$mock_bin:$PATH" \
    MOCK_LOG="$LOG_FILE" \
    MOCK_STATE_DIR="$STATE_DIR" \
    WEAVEPRESS_APP_DIR="$APP_DIR" \
    WEAVEPRESS_LOCK_FILE="$LOCK_FILE" \
    "$initialize"
}

assert_env_contract() {
  local env_file="$1"
  local keys_file="$CASE_DIR/keys"
  awk -F= '/^[A-Z0-9_]+=/ { print $1 }' "$env_file" > "$keys_file"
  diff -u <(awk -F= '/^[A-Z0-9_]+=/ { print $1 }' "$APP_DIR/.env.example") "$keys_file" >/dev/null || fail '.env does not have the exact 14-key contract'
  [[ "$(stat -c '%a' "$env_file")" == "600" ]] || fail '.env mode is not 0600'
}

test_initialize_happy_path() {
  new_case initialize-happy
  output="$CASE_DIR/output"
  run_initialize > "$output" 2>&1 || fail 'initialize happy path failed'
  [[ -f "$APP_DIR/.env" ]] || fail 'initialize did not install .env'
  assert_env_contract "$APP_DIR/.env"
  grep -Fq 'SQL_OP=CREATE_SCHEMA' "$LOG_FILE" || fail 'initialize did not create the schema'
  grep -Fq 'SQL_OP=GRANT_APP' "$LOG_FILE" || fail 'initialize did not grant the app user'
  grep -Fq 'SQL_OP=GRANT_MIGRATOR' "$LOG_FILE" || fail 'initialize did not grant the migrator user'
  compose_line="$(grep -n ' compose .* config --quiet' "$LOG_FILE" | head -n1 | cut -d: -f1)"
  schema_line="$(grep -n 'SQL_OP=CREATE_SCHEMA' "$LOG_FILE" | head -n1 | cut -d: -f1)"
  [[ -n "$compose_line" && "$compose_line" -lt "$schema_line" ]] || fail 'initialize did not validate staged Compose env before DB writes'
  if grep -Eq 'RedisSafe_123|000000000000000000000000000000000000000000000000000000000000000[1-4]' "$LOG_FILE" "$output"; then
    fail 'initialize exposed a generated or Redis secret in logs'
  fi
}

test_initialize_compensates_partial_ddl() {
  new_case initialize-compensation
  output="$CASE_DIR/output"
  if MOCK_FAIL_SQL_OP=GRANT_MIGRATOR run_initialize > "$output" 2>&1; then
    fail 'initialize accepted a failed migrator grant'
  fi
  [[ ! -e "$APP_DIR/.env" ]] || fail 'initialize installed .env after failed DDL'
  expected="$CASE_DIR/expected-ops"
  actual="$CASE_DIR/actual-ops"
  printf '%s\n' CREATE_SCHEMA CREATE_APP_USER GRANT_APP CREATE_MIGRATOR_USER GRANT_MIGRATOR DROP_MIGRATOR_USER DROP_APP_USER DROP_SCHEMA > "$expected"
  sed -n 's/^SQL_OP=//p' "$LOG_FILE" > "$actual"
  diff -u "$expected" "$actual" >/dev/null || fail 'initialize compensation did not remove only this run objects in reverse order'
}

test_initialize_no_replace() {
  new_case initialize-no-replace
  output="$CASE_DIR/output"
  if MOCK_CREATE_ENV_AFTER_SQL_OP=GRANT_MIGRATOR run_initialize > "$output" 2>&1; then
    fail 'initialize overwrote an .env created during validation'
  fi
  [[ "$(<"$APP_DIR/.env")" == 'sentinel-existing-env' ]] || fail 'initialize replaced the concurrent .env target'
  grep -Fq 'SQL_OP=DROP_SCHEMA' "$LOG_FILE" || fail 'initialize did not compensate after atomic no-replace failure'
}

test_initialize_commit_survives_staging_cleanup_failure() {
  new_case initialize-commit-cleanup-failure
  if ! MOCK_FAIL_ENV_STAGE_REMOVE=1 run_initialize > "$CASE_DIR/output" 2>&1; then
    fail 'initialize reported failure after .env had been atomically committed'
  fi
  [[ -f "$APP_DIR/.env" ]] || fail 'initialize removed the committed .env after staging cleanup failed'
  assert_env_contract "$APP_DIR/.env"
  if grep -Eq '^SQL_OP=DROP_' "$LOG_FILE"; then
    fail 'initialize compensated committed database objects after staging cleanup failed'
  fi
}

test_initialize_commit_survives_signal_after_link() {
  new_case initialize-commit-signal
  if MOCK_SIGNAL_AFTER_ENV_LINK=1 run_initialize > "$CASE_DIR/output" 2>&1; then
    fail 'initialize ignored SIGTERM after committing .env'
  fi
  [[ -f "$APP_DIR/.env" ]] || fail 'initialize removed the committed .env after SIGTERM'
  assert_env_contract "$APP_DIR/.env"
  if grep -Eq '^SQL_OP=DROP_' "$LOG_FILE"; then
    fail 'initialize compensated committed database objects after SIGTERM'
  fi
}

test_initialize_rejects_preexisting_database_objects() {
  local name variable
  local fixtures=(
    existing-schema MOCK_SCHEMA_COUNT
    existing-app-user MOCK_APP_USER_COUNT
    existing-migrator-user MOCK_MIGRATOR_USER_COUNT
  )

  for ((i = 0; i < ${#fixtures[@]}; i += 2)); do
    name="${fixtures[i]}"
    variable="${fixtures[i + 1]}"
    new_case "initialize-$name"
    if env "$variable=1" \
      PATH="$mock_bin:$PATH" \
      MOCK_LOG="$LOG_FILE" \
      MOCK_STATE_DIR="$STATE_DIR" \
      WEAVEPRESS_APP_DIR="$APP_DIR" \
      WEAVEPRESS_LOCK_FILE="$LOCK_FILE" \
      "$initialize" > "$CASE_DIR/output" 2>&1; then
      fail "initialize accepted pre-existing database object: $name"
    fi
    [[ ! -e "$APP_DIR/.env" ]] || fail "initialize wrote .env with pre-existing database object: $name"
    if grep -q '^SQL_OP=' "$LOG_FILE"; then
      fail "initialize wrote or compensated database objects after preflight rejection: $name"
    fi
  done
}

test_initialize_rejects_invalid_staging_and_redis_secret() {
  new_case initialize-compose-failure
  if MOCK_COMPOSE_CONFIG_FAIL=1 run_initialize > "$CASE_DIR/output" 2>&1; then
    fail 'initialize accepted a staged environment that failed Compose validation'
  fi
  [[ ! -e "$APP_DIR/.env" ]] || fail 'initialize installed .env after Compose validation failed'
  if grep -q '^SQL_OP=' "$LOG_FILE"; then fail 'initialize wrote database objects before Compose validation passed'; fi
  if grep -Fq 'SENSITIVE_COMPOSE_DIAGNOSTIC' "$CASE_DIR/output"; then fail 'initialize exposed Compose diagnostics'; fi

  local name value
  local fixtures=(quote "Redis'Password" backslash 'Redis\Password')
  for ((i = 0; i < ${#fixtures[@]}; i += 2)); do
    name="${fixtures[i]}"
    value="${fixtures[i + 1]}"
    new_case "initialize-redis-$name"
    if MOCK_REDIS_PASSWORD="$value" run_initialize > "$CASE_DIR/output" 2>&1; then
      fail "initialize accepted a Redis password containing $name"
    fi
    [[ ! -e "$APP_DIR/.env" ]] || fail "initialize wrote .env for a Redis password containing $name"
    if grep -q '^SQL_OP=' "$LOG_FILE"; then fail "initialize wrote database objects for a Redis password containing $name"; fi
  done
}

test_initialize_rejects_extra_dotenv_assignment() {
  new_case initialize-extra-dotenv-assignment
  printf 'lowercase_extra=must-be-rejected\n' >> "$APP_DIR/.env.example"
  if run_initialize > "$CASE_DIR/output" 2>&1; then
    fail 'initialize accepted an extra lowercase dotenv assignment'
  fi
  [[ ! -e "$APP_DIR/.env" ]] || fail 'initialize wrote .env after accepting an extra dotenv assignment'
  if grep -q '^SQL_OP=' "$LOG_FILE"; then fail 'initialize wrote database objects before rejecting an extra dotenv assignment'; fi
}

write_runtime_env() {
  cp "$APP_DIR/.env.example" "$APP_DIR/.env"
  chmod 0600 "$APP_DIR/.env"
}

run_deploy() {
  local stdin_file="$1"
  shift
  PATH="$mock_bin:$PATH" \
    MOCK_LOG="$LOG_FILE" \
    MOCK_STATE_DIR="$STATE_DIR" \
    WEAVEPRESS_APP_DIR="$APP_DIR" \
    WEAVEPRESS_LOCK_FILE="$LOCK_FILE" \
    WEAVEPRESS_METADATA_DIR="$METADATA_DIR" \
    WEAVEPRESS_INPUT_TIMEOUT=1 \
    "$@" "$deploy" 0123456789abcdef0123456789abcdef01234567 --component=gateway < "$stdin_file"
}

run_server_deploy() {
  local stdin_file="$1"
  shift
  PATH="$mock_bin:$PATH" \
    MOCK_LOG="$LOG_FILE" \
    MOCK_STATE_DIR="$STATE_DIR" \
    WEAVEPRESS_APP_DIR="$APP_DIR" \
    WEAVEPRESS_LOCK_FILE="$LOCK_FILE" \
    WEAVEPRESS_METADATA_DIR="$METADATA_DIR" \
    WEAVEPRESS_INPUT_TIMEOUT=1 \
    WEAVEPRESS_HEALTH_ATTEMPTS=1 \
    WEAVEPRESS_HEALTH_DELAY=1 \
    "$@" "$deploy" 0123456789abcdef0123456789abcdef01234567 --component=server < "$stdin_file"
}

test_deploy_framing_before_lock() {
  local input output long_user

  new_case deploy-timeout
  write_runtime_env
  output="$CASE_DIR/output"
  if PATH="$mock_bin:$PATH" MOCK_LOG="$LOG_FILE" MOCK_STATE_DIR="$STATE_DIR" WEAVEPRESS_APP_DIR="$APP_DIR" WEAVEPRESS_LOCK_FILE="$LOCK_FILE" WEAVEPRESS_METADATA_DIR="$METADATA_DIR" WEAVEPRESS_INPUT_TIMEOUT=1 \
    "$deploy" 0123456789abcdef0123456789abcdef01234567 --component=gateway < <(sleep 2) > "$output" 2>&1; then
    fail 'deploy accepted a timed-out credential frame'
  fi
  [[ ! -e "$LOCK_FILE" ]] || fail 'deploy acquired/opened its lock before timed input completed'

  new_case deploy-overlong
  write_runtime_env
  input="$CASE_DIR/input"
  printf -v long_user '%257s' ''
  long_user="${long_user// /u}"
  printf '%s\nPasswordSafe_123\n' "$long_user" > "$input"
  if run_deploy "$input" env > "$CASE_DIR/output" 2>&1; then
    fail 'deploy accepted an overlong ACR username'
  fi
  [[ ! -e "$LOCK_FILE" ]] || fail 'deploy acquired/opened its lock before rejecting an overlong frame'

  new_case deploy-extra
  write_runtime_env
  input="$CASE_DIR/input"
  printf 'acr-user\nPasswordSafe_123\nextra\n' > "$input"
  if run_deploy "$input" env > "$CASE_DIR/output" 2>&1; then
    fail 'deploy accepted extra stdin bytes'
  fi
  [[ ! -e "$LOCK_FILE" ]] || fail 'deploy acquired/opened its lock before rejecting extra stdin bytes'
}

test_deploy_happy_metadata_and_cleanup() {
  new_case deploy-happy
  write_runtime_env
  input="$CASE_DIR/input"
  printf 'acr-user\nPasswordSafe_123\n' > "$input"
  run_deploy "$input" env > "$CASE_DIR/output" 2>&1 || fail 'gateway deploy happy path failed'
  metadata="$METADATA_DIR/gateway.env"
  [[ -f "$metadata" && "$(stat -c '%a' "$metadata")" == "600" ]] || fail 'gateway metadata missing or not 0600'
  grep -Fxq 'APP_SHA=0123456789abcdef0123456789abcdef01234567' "$metadata" || fail 'gateway metadata SHA is incorrect'
  docker_config="$(sed -n 's/^DOCKER_CONFIG=\([^ ]*\) CMD=docker login.*/\1/p' "$LOG_FILE" | head -n1)"
  [[ -n "$docker_config" && ! -e "$docker_config" ]] || fail 'temporary DOCKER_CONFIG was not removed'
  if grep -Fq 'PasswordSafe_123' "$LOG_FILE" "$CASE_DIR/output"; then fail 'ACR password appeared in logs'; fi
  if find "$METADATA_DIR" -maxdepth 1 -name '.*.env.*' -print -quit | grep -q .; then fail 'metadata temp file was left behind'; fi
}

test_gateway_first_deploy_failure_stops_component() {
  new_case gateway-first-failure
  write_runtime_env
  input="$CASE_DIR/input"
  printf 'acr-user\nPasswordSafe_123\n' > "$input"
  if MOCK_CURL_FAILURES=1 run_deploy "$input" env > "$CASE_DIR/output" 2>&1; then
    fail 'gateway deploy reported success after smoke failure'
  fi
  grep -Eq ' compose .* stop gateway' "$LOG_FILE" || fail 'first gateway failure did not stop the requested component'
  [[ ! -e "$METADATA_DIR/gateway.env" ]] || fail 'failed gateway deploy wrote release metadata'
}

test_gateway_old_version_rollback_is_verified() {
  new_case gateway-rollback
  write_runtime_env
  input="$CASE_DIR/input"
  printf 'acr-user\nPasswordSafe_123\n' > "$input"
  if MOCK_GATEWAY_OLD=1 MOCK_CURL_FAILURES=1 run_deploy "$input" env > "$CASE_DIR/output" 2>&1; then
    fail 'gateway deploy hid the original smoke failure'
  fi
  grep -Eq 'docker image tag sha256:old-gateway .*weavepress-gateway:rollback-' "$LOG_FILE" || fail 'gateway rollback did not tag the old image'
  [[ "$(grep -c 'Config.Image' "$LOG_FILE")" -ge 1 ]] || fail 'gateway rollback did not verify the restored image reference'
  [[ "$(grep -c '^CMD=curl' "$LOG_FILE")" -ge 2 ]] || fail 'gateway rollback did not rerun the smoke check'
  grep -Fq 'Application containers restored' "$CASE_DIR/output" || fail 'verified gateway rollback was not reported'
}

test_gateway_failed_rollback_is_not_reported_as_restored() {
  new_case gateway-rollback-failure
  write_runtime_env
  input="$CASE_DIR/input"
  printf 'acr-user\nPasswordSafe_123\n' > "$input"
  if MOCK_GATEWAY_OLD=1 MOCK_CURL_FAILURES=2 run_deploy "$input" env > "$CASE_DIR/output" 2>&1; then
    fail 'gateway deploy reported success after deployment and rollback smoke failures'
  fi
  if grep -Fq 'Application containers restored' "$CASE_DIR/output"; then
    fail 'failed gateway rollback was reported as restored'
  fi
  grep -Fq 'Automatic application rollback failed' "$CASE_DIR/output" || fail 'failed gateway rollback did not require manual recovery'
}

test_server_first_deploy_health_failure_stops_components() {
  new_case server-first-health-failure
  write_runtime_env
  input="$CASE_DIR/input"
  printf 'acr-user\nPasswordSafe_123\n' > "$input"
  if MOCK_FAIL_NEW_SERVER_HEALTH=1 run_server_deploy "$input" env > "$CASE_DIR/output" 2>&1; then
    fail 'server first deploy reported success after API health failure'
  fi
  grep -Eq ' compose .* stop api worker' "$LOG_FILE" || fail 'server first deploy failure did not stop API and Worker'
  [[ ! -e "$METADATA_DIR/server.env" ]] || fail 'failed server first deploy wrote release metadata'
}

test_server_old_version_health_failure_rolls_back_and_verifies() {
  new_case server-old-health-failure
  write_runtime_env
  input="$CASE_DIR/input"
  printf 'acr-user\nPasswordSafe_123\n' > "$input"
  if MOCK_SERVER_OLD=1 MOCK_FAIL_NEW_SERVER_HEALTH=1 run_server_deploy "$input" env > "$CASE_DIR/output" 2>&1; then
    fail 'server deploy hid the requested version health failure'
  fi
  grep -Eq 'docker image tag sha256:old-api .*weavepress-api:rollback-' "$LOG_FILE" || fail 'server rollback did not tag the old API image'
  grep -Eq 'docker image tag sha256:old-worker .*weavepress-worker:rollback-' "$LOG_FILE" || fail 'server rollback did not tag the old Worker image'
  [[ "$(grep -c 'Config.Image' "$LOG_FILE")" -ge 2 ]] || fail 'server rollback did not verify both restored image references'
  grep -Fq 'Application containers restored' "$CASE_DIR/output" || fail 'verified server rollback was not reported'
  [[ ! -e "$METADATA_DIR/server.env" ]] || fail 'failed server deploy wrote release metadata'
}

test_deploy_rejects_invalid_health_bounds_before_lock() {
  local variable value
  local fixtures=(
    WEAVEPRESS_HEALTH_ATTEMPTS 0
    WEAVEPRESS_HEALTH_ATTEMPTS 121
    WEAVEPRESS_HEALTH_ATTEMPTS invalid
    WEAVEPRESS_HEALTH_DELAY 0
    WEAVEPRESS_HEALTH_DELAY 31
    WEAVEPRESS_HEALTH_DELAY invalid
  )

  for ((i = 0; i < ${#fixtures[@]}; i += 2)); do
    variable="${fixtures[i]}"
    value="${fixtures[i + 1]}"
    new_case "deploy-invalid-$i"
    write_runtime_env
    input="$CASE_DIR/input"
    printf 'acr-user\nPasswordSafe_123\n' > "$input"
    if run_deploy "$input" env "$variable=$value" > "$CASE_DIR/output" 2>&1; then
      fail "deploy accepted invalid health bound: $variable=$value"
    fi
    [[ ! -e "$LOCK_FILE" ]] || fail "deploy opened its lock before rejecting $variable=$value"
    [[ ! -s "$LOG_FILE" ]] || fail "deploy invoked Docker before rejecting $variable=$value"
  done
}

test_external_validators_and_lock() {
  if ! bash -c '
    set -Eeuo pipefail
    source "$1"
    declare -F write_dotenv_value >/dev/null
    for bad in "quote'"'"'value" "backslash\\" $'"'"'tab\tvalue'"'"' $'"'"'escape\033value'"'"' $'"'"'delete\177value'"'"'; do
      if write_dotenv_value TEST "$bad" access >/dev/null 2>&1; then exit 1; fi
    done
    write_dotenv_value TEST "Safe_Value-123.abc" access >/dev/null
    if write_dotenv_value TEST "http://example.invalid" domain >/dev/null 2>&1; then exit 1; fi
    write_dotenv_value TEST "https://cdn.example.invalid/path?a=1#fragment" domain >/dev/null
  ' bash "$external"; then
    fail 'external dotenv validators accepted unsafe quote/backslash/control/domain input'
  fi

  command -v script >/dev/null 2>&1 || fail 'script(1) is required for external installer TTY behavior tests'

  new_case external-happy
  write_runtime_env
  command_line="env PATH='$mock_bin:$PATH' MOCK_LOG='$LOG_FILE' MOCK_STATE_DIR='$STATE_DIR' WEAVEPRESS_APP_DIR='$APP_DIR' WEAVEPRESS_LOCK_FILE='$LOCK_FILE' WEAVEPRESS_METADATA_DIR='$METADATA_DIR' '$external'"
  printf '%s\n' QiniuAK_123 QiniuSK_123 bucket-1 https://cdn.example.invalid '' '' \
    | script -qec "$command_line" /dev/null > "$CASE_DIR/output" 2>&1 || fail 'external installer happy path failed under a TTY'
  [[ -e "$LOCK_FILE" ]] || fail 'external installer did not acquire the shared deploy lock'
  grep -Fxq 'WEAVEPRESS_APP_ENV=production' "$APP_DIR/.env" || fail 'external installer did not set production mode'
  if grep -Eq ' compose .* (up|run|restart|start) ' "$LOG_FILE"; then fail 'external installer started a service'; fi

  new_case external-compose-failure
  write_runtime_env
  cp "$APP_DIR/.env" "$CASE_DIR/env-before"
  command_line="env PATH='$mock_bin:$PATH' MOCK_LOG='$LOG_FILE' MOCK_STATE_DIR='$STATE_DIR' MOCK_COMPOSE_CONFIG_FAIL=1 WEAVEPRESS_APP_DIR='$APP_DIR' WEAVEPRESS_LOCK_FILE='$LOCK_FILE' WEAVEPRESS_METADATA_DIR='$METADATA_DIR' '$external'"
  if printf '%s\n' QiniuAK_123 QiniuSK_123 bucket-1 https://cdn.example.invalid '' '' \
    | script -qec "$command_line" /dev/null > "$CASE_DIR/output" 2>&1; then
    fail 'external installer accepted a staged environment that failed Compose validation'
  fi
  cmp -s "$CASE_DIR/env-before" "$APP_DIR/.env" || fail 'external installer replaced .env after Compose validation failed'
  if grep -Fq 'SENSITIVE_COMPOSE_DIAGNOSTIC' "$CASE_DIR/output"; then fail 'external installer exposed Compose diagnostics'; fi
  if find "$APP_DIR" -maxdepth 1 \( -name '.env.external.*' -o -name '.compose-external-error.*' \) -print -quit | grep -q .; then
    fail 'external installer left a staging or diagnostic temp file after failure'
  fi

  new_case external-extra-dotenv-assignment
  write_runtime_env
  printf 'lowercase_extra=must-be-rejected\n' >> "$APP_DIR/.env"
  cp "$APP_DIR/.env" "$CASE_DIR/env-before"
  command_line="env PATH='$mock_bin:$PATH' MOCK_LOG='$LOG_FILE' MOCK_STATE_DIR='$STATE_DIR' WEAVEPRESS_APP_DIR='$APP_DIR' WEAVEPRESS_LOCK_FILE='$LOCK_FILE' WEAVEPRESS_METADATA_DIR='$METADATA_DIR' '$external'"
  if printf '%s\n' QiniuAK_123 QiniuSK_123 bucket-1 https://cdn.example.invalid '' '' \
    | script -qec "$command_line" /dev/null > "$CASE_DIR/output" 2>&1; then
    fail 'external installer accepted an extra lowercase dotenv assignment'
  fi
  cmp -s "$CASE_DIR/env-before" "$APP_DIR/.env" || fail 'external installer replaced .env after an extra dotenv assignment'
}

write_mocks
test_initialize_happy_path
test_initialize_compensates_partial_ddl
test_initialize_no_replace
test_initialize_commit_survives_staging_cleanup_failure
test_initialize_commit_survives_signal_after_link
test_initialize_rejects_preexisting_database_objects
test_initialize_rejects_invalid_staging_and_redis_secret
test_initialize_rejects_extra_dotenv_assignment
test_deploy_framing_before_lock
test_deploy_rejects_invalid_health_bounds_before_lock
test_deploy_happy_metadata_and_cleanup
test_gateway_first_deploy_failure_stops_component
test_gateway_old_version_rollback_is_verified
test_gateway_failed_rollback_is_not_reported_as_restored
test_server_first_deploy_health_failure_stops_components
test_server_old_version_health_failure_rolls_back_and_verifies
test_external_validators_and_lock
printf 'Production script behavior tests passed.\n'
