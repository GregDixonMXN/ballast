#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ ${1:-} != --skip-build ]]; then ./scripts/build.sh; fi
version=$(cat VERSION)
platform="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)"
name="ballast-$version-$platform"
staging=$(mktemp -d)
trap 'rm -rf -- "$staging"' EXIT
root="$staging/$name"
mkdir -p "$root/bin" "$root/web/.next" "$root/scripts" dist
cp bin/ballast bin/ballast-server bin/ballast-runner "$root/bin/"
cp -a apps/web/.next/standalone/. "$root/web/"
cp -a apps/web/.next/static "$root/web/.next/static"
if [[ -d apps/web/public ]]; then cp -a apps/web/public "$root/web/public"; fi
cp -a docs "$root/"
cp README.md VERSION CHANGELOG.md SECURITY.md CONTRIBUTING.md "$root/"
cp scripts/start.sh "$root/scripts/"
python3 scripts/notices.py "$root/THIRD_PARTY_NOTICES.md"
cp apps/web/package-lock.json "$root/web/dependency-lock.json"
(cd "$root" && find bin web -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
tar -czf "dist/$name.tar.gz" -C "$staging" "$name"
(cd dist && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
printf 'Release candidate: dist/%s.tar.gz\n' "$name"
