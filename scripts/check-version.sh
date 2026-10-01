#!/usr/bin/env bash
# Fail when the top CHANGELOG entry doesn't match VERSION.
# Usage: scripts/check-version.sh
set -euo pipefail
cd "$(dirname "$0")/.."
version=$(tr -d '[:space:]' < VERSION)
top=$(grep -m1 -oE '^## [0-9][0-9A-Za-z.\-]*' CHANGELOG.md | awk '{print $2}')
if [[ -z $top ]]; then
  echo "CHANGELOG has no versioned entry (expected '## <version>')" >&2
  exit 1
fi
if [[ $version != "$top" ]]; then
  echo "VERSION ($version) != top CHANGELOG entry ($top)" >&2
  exit 1
fi
echo "version sync: $version"
