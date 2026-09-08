#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)
PROJECT_ROOT=$(cd -- "${SCRIPT_DIR}/../.." >/dev/null 2>&1 && pwd)
IMAGE_NAME="nova-admin-local"

docker build "${PROJECT_ROOT}" \
  --file "${SCRIPT_DIR}/Dockerfile" \
  --tag "${IMAGE_NAME}"

echo "Docker image '${IMAGE_NAME}' built successfully."
echo "Run: docker run -d -p 8010:8080 --name ${IMAGE_NAME} ${IMAGE_NAME}"
