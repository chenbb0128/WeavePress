#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gateway="$repo_root/deploy/nginx.conf"
compose_file="$repo_root/deploy/production/compose.yaml"

grep -Fq 'location = /health {' "$gateway"
grep -Fq 'location = /ready {' "$gateway"
grep -Fq 'location = /metrics {' "$gateway"
grep -Fq 'return 404;' "$gateway"

if grep -Eq '^[[:space:]]+(mysql|redis|nginx):' "$compose_file"; then
  printf 'Production compose must reuse shared MySQL, Redis and Nginx.\n' >&2
  exit 1
fi
if grep -Eq '^[[:space:]]+ports:' "$compose_file"; then
  printf 'Application services must not publish host ports.\n' >&2
  exit 1
fi

docker run --rm --add-host api:127.0.0.1 \
  -v "$gateway:/etc/nginx/conf.d/default.conf:ro" \
  nginx:1.27-alpine nginx -t

APP_SHA=0000000000000000000000000000000000000000 \
  docker compose --env-file "$repo_root/deploy/production/.env.example" \
  -f "$compose_file" config --quiet
