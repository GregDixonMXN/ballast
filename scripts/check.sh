#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
GO_BIN=${GO:-go}
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
# DATABASE_URL may refer to an operator's real deployment. Tests use an
# explicitly separate BALLAST_TEST_DATABASE_URL fixture only.
unset DATABASE_URL
formatting=$("$(dirname "$(command -v "$GO_BIN")")/gofmt" -l cmd internal migrations)
if [[ -n $formatting ]]; then printf 'Unformatted Go files:\n%s\n' "$formatting"; exit 1; fi
"$GO_BIN" vet ./...
"$GO_BIN" test -race -count=1 ./...
npm ci --prefix apps/web
npm run typecheck --prefix apps/web
npm run build --prefix apps/web
git diff --check
