#!/usr/bin/env bash
# Creates and removes its own ephemeral fixture; never uses DATABASE_URL.
set -euo pipefail
cd "$(dirname "$0")/.."
name="ballast-test-pg-$$"
cleanup() { docker stop "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
docker run --rm -d --name "$name" -e POSTGRES_USER=ballast_test -e POSTGRES_DB=ballast_test -e POSTGRES_HOST_AUTH_METHOD=trust -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
for ((attempt=0; attempt<60; attempt++)); do
  if docker exec "$name" pg_isready -U ballast_test -d ballast_test >/dev/null 2>&1; then break; fi
  sleep 1
done
port=$(docker port "$name" 5432/tcp | sed 's/.*://')
export BALLAST_TEST_DATABASE_URL="postgres://ballast_test@127.0.0.1:$port/ballast_test?sslmode=disable"
unset DATABASE_URL
"${GO:-go}" test -race -count=1 ./internal/store
