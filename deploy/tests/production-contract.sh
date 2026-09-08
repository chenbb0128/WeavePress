#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gateway="$repo_root/deploy/nginx.conf"
compose_file="$repo_root/deploy/production/compose.yaml"
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
      depth += gsub(/{/, "", opening_line)
      depth -= gsub(/}/, "", closing_line)
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

require_in_block() {
  local block_file="$1"
  local expected="$2"
  local location_header="$3"

  if ! grep -Fq "$expected" "$block_file"; then
    printf '%s must contain: %s\n' "$location_header" "$expected" >&2
    return 1
  fi
}

reject_in_block() {
  local block_file="$1"
  local rejected="$2"
  local location_header="$3"

  if grep -Fq "$rejected" "$block_file"; then
    printf '%s must not contain: %s\n' "$location_header" "$rejected" >&2
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
  require_in_block "$block_file" 'proxy_pass http://api:8080;' "$location_header" || return 1
  require_in_block "$block_file" 'proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;' "$location_header" || return 1
}

assert_metrics_location() {
  local gateway_file="$1"
  local location_header='location = /metrics'
  local block_file="$contract_temp_dir/metrics.location"

  if ! extract_location_block "$location_header" "$gateway_file" "$block_file"; then
    printf 'Missing Nginx location block: %s\n' "$location_header" >&2
    return 1
  fi
  require_in_block "$block_file" 'access_log off;' "$location_header" || return 1
  require_in_block "$block_file" 'return 404;' "$location_header" || return 1
  reject_in_block "$block_file" 'proxy_pass ' "$location_header" || return 1
}

assert_gateway_contract() {
  local gateway_file="$1"

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
        depth += gsub(/{/, "", opening_line)
        depth -= gsub(/}/, "", closing_line)
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

  assert_gateway_contract "$gateway"

  mutate_location_literal "$gateway" 'location = /health' \
    'proxy_pass http://api:8080;' 'proxy_pass http://wrong-api:8080;' "$health_mutant"
  mutate_location_literal "$gateway" 'location = /metrics' \
    'return 404;' $'proxy_pass http://api:8080;\n        return 404;' "$metrics_mutant"
  mutate_location_literal "$gateway" 'location /api/' \
    'proxy_set_header X-Forwarded-Proto $http_x_forwarded_proto;' \
    'proxy_set_header X-Forwarded-Proto $scheme;' "$proto_mutant"

  expect_gateway_rejection 'health upstream changed' "$health_mutant"
  expect_gateway_rejection 'metrics proxied' "$metrics_mutant"
  expect_gateway_rejection 'X-Forwarded-Proto changed to $scheme' "$proto_mutant"
}

assert_compose_contract() {
  if grep -Eq '^[[:space:]]+(mysql|redis|nginx):' "$compose_file"; then
    printf 'Production compose must reuse shared MySQL, Redis and Nginx.\n' >&2
    return 1
  fi
  if grep -Eq '^[[:space:]]+ports:' "$compose_file"; then
    printf 'Application services must not publish host ports.\n' >&2
    return 1
  fi
}

case "${1:-}" in
  --gateway-only)
    if [[ $# -ne 2 ]]; then
      printf 'Usage: %s --gateway-only <nginx.conf>\n' "$0" >&2
      exit 2
    fi
    assert_gateway_contract "$2"
    ;;
  --self-test)
    if [[ $# -ne 1 ]]; then
      printf 'Usage: %s --self-test\n' "$0" >&2
      exit 2
    fi
    run_gateway_self_test
    ;;
  '')
    assert_gateway_contract "$gateway"
    assert_compose_contract

    docker run --rm --add-host api:127.0.0.1 \
      -v "$gateway:/etc/nginx/conf.d/default.conf:ro" \
      nginx:1.27-alpine nginx -t

    APP_SHA=0000000000000000000000000000000000000000 \
      docker compose --env-file "$repo_root/deploy/production/.env.example" \
      -f "$compose_file" config --quiet
    ;;
  *)
    printf 'Unknown option: %s\n' "$1" >&2
    exit 2
    ;;
esac
