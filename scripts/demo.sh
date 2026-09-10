#!/usr/bin/env bash
# A full, isolated API workflow, not a mutation of an existing repository.
set -euo pipefail
cd "$(dirname "$0")/.."
build=$(mktemp -d)
trap 'rm -rf -- "$build"' EXIT
"${GO:-go}" build -o "$build/ballast-server" ./cmd/server
python3 tests/release_smoke.py --bin-dir "$build"
