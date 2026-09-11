#!/usr/bin/env bash
# Ballast overlap demo: two writers, one file, no model, no keys.
# First integrate wins; the loser is marked NEEDS_REBASE/CONFLICTED —
# never silently force-merged. Exit 0 only if that story holds.
set -euo pipefail
cd "$(dirname "$0")/.."
build=$(mktemp -d)
trap 'rm -rf -- "$build"' EXIT
"${GO:-go}" build -o "$build/ballast-server" ./cmd/server
"${GO:-go}" build -o "$build/ballast-runner" ./cmd/runner
python3 tests/demo_overlap.py "$build"
