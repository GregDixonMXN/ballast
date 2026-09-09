#!/usr/bin/env bash
# One-command local boot: postgres + migrate + api + runner + web.
set -euo pipefail
cd "$(dirname "$0")/.."

: "${DATABASE_URL:=postgres://ballast:ballast@localhost:5432/ballast?sslmode=disable}"
export DATABASE_URL

if command -v docker >/dev/null 2>&1; then
  echo "[dev] starting postgres via compose…"
  docker compose -f docker/compose.yml up -d postgres
else
  echo "[dev] docker not found; using local postgres at $DATABASE_URL"
fi

echo "[dev] building…"
go build ./...

echo "[dev] starting api :8080 (background)…"
go run ./cmd/server --addr :8080 &
API_PID=$!
echo "[dev] starting runner (background)…"
go run ./cmd/runner --server http://localhost:8080 &
RUNNER_PID=$!

if [ -d apps/web ] && command -v npm >/dev/null 2>&1; then
  echo "[dev] starting web :3000 (background)…"
  (cd apps/web && npm run dev) &
  WEB_PID=$!
fi

echo "[dev] api=$API_PID runner=$RUNNER_PID ${WEB_PID:-noweeb}"
echo "[dev] health: curl localhost:8080/healthz"
echo "[dev] stop: kill $API_PID $RUNNER_PID ${WEB_PID:-}"
wait
