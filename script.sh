#!/usr/bin/env bash
set -euo pipefail

CONTAINER_NAME="laika"
IMAGE_NAME="laika"

# 1. Check .env exists
if [ ! -f .env ]; then
  echo "Error: .env not found. Copy .env.example and fill in values:"
  echo "  cp .env.example .env"
  exit 1
fi

# Load PORT from .env (default 8080 if not set)
PORT=$(grep -E '^PORT=' .env | cut -d'=' -f2 | tr -d '[:space:]')
PORT=${PORT:-8080}

# 2. Check for a running container
if docker ps -q --filter "name=^${CONTAINER_NAME}$" | grep -q .; then
  echo "Stopping running container: ${CONTAINER_NAME}"
  docker stop "${CONTAINER_NAME}"
fi

# 3. Remove the container if it exists (stopped or running)
if docker ps -aq --filter "name=^${CONTAINER_NAME}$" | grep -q .; then
  echo "Removing container: ${CONTAINER_NAME}"
  docker rm "${CONTAINER_NAME}"
fi

# 4. Build new image
echo "Building image: ${IMAGE_NAME}"
docker build --build-arg PORT="${PORT}" -t "${IMAGE_NAME}" .

# 5. Deploy
echo "Starting container: ${CONTAINER_NAME} on port ${PORT}"
docker run -d \
  --name "${CONTAINER_NAME}" \
  --env-file .env \
  -p "${PORT}:${PORT}" \
  --restart unless-stopped \
  "${IMAGE_NAME}"

echo "Done. Container is running on http://localhost:${PORT}"
