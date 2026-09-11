#!/usr/bin/env bash
# Ballast green demo: one task, one file, end to end, no model.
set -euo pipefail
cd "$(dirname "$0")/.."
build=$(mktemp -d)
trap 'rm -rf -- "$build"' EXIT
"${GO:-go}" build -o "$build/ballast-server" ./cmd/server
"${GO:-go}" build -o "$build/ballast-runner" ./cmd/runner
python3 tests/demo_green.py "$build"
