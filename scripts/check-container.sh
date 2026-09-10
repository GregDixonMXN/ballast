#!/usr/bin/env bash
# Verify a built archive as an unprivileged user in a minimal Node/Git runtime.
set -euo pipefail
cd "$(dirname "$0")/.."
archive=${1:-"dist/ballast-$(cat VERSION)-linux-x86_64.tar.gz"}
staging=$(mktemp -d)
trap 'rm -rf -- "$staging"' EXIT
chmod 755 "$staging"
tar -xzf "$archive" -C "$staging"
name=$(basename "$archive" .tar.gz)
(cd "$staging/$name" && sha256sum -c SHA256SUMS >/dev/null)
docker build -t ballast-runtime-check:local -f docker/Dockerfile.runtime-check .
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges --tmpfs /tmp:rw,exec,nosuid,size=512m -v "$staging:/release:ro" -v "$PWD/tests:/checks:ro" ballast-runtime-check:local bash /checks/package_runtime.sh "/release/$name"
