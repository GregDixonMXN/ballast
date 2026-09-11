#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
GO_BIN=${GO:-go}
version=$(cat VERSION)
mkdir -p bin
for component in ballast server runner supervise; do
  output=$component
  if [[ $component != ballast ]]; then output=ballast-$component; fi
  CGO_ENABLED=0 "$GO_BIN" build -trimpath -ldflags "-s -w -X main.version=$version" -o "bin/$output" "./cmd/$component"
done
npm ci --prefix apps/web
npm run build --prefix apps/web
