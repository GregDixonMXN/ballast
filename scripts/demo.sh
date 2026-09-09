#!/usr/bin/env bash
# Demo: temp repo → two tasks → two worktrees → separate edits (clean) →
# same-file edit (conflict) → changeset inspection. Mirrors
# internal/integration/flow_test.go without needing the API.
#
# NOTE: native Windows git cannot resolve MSYS /tmp or /c/... paths, so
# scratch lives under the repo and every git invocation gets a native
# C:/... path via cygpath. Kept after the run for inspection; delete
# .demo-* when done.
set -euo pipefail
cd "$(dirname "$0")/.."

ROOT="$PWD/.demo-$RANDOM"
NATIVE_ROOT=$(cygpath -m "$ROOT")
REPO=$ROOT/repo
NATIVE_REPO=$(cygpath -m "$REPO")
NATIVE_WT_A=$NATIVE_ROOT/wt/a
NATIVE_WT_B=$NATIVE_ROOT/wt/b
mkdir -p "$REPO" "$ROOT/wt"
echo "[demo] root $ROOT"
git init -b main -q "$NATIVE_REPO"
git -C "$NATIVE_REPO" config user.email demo@ballast.dev
git -C "$NATIVE_REPO" config user.name ballast-demo
echo "package api" > "$REPO/backend.go"
echo "export {}" > "$REPO/frontend.tsx"
git -C "$NATIVE_REPO" add -A
git -C "$NATIVE_REPO" commit -qm init
BASE=$(git -C "$NATIVE_REPO" rev-parse HEAD)
echo "[demo] base $BASE"

git -C "$NATIVE_REPO" worktree add --detach "$NATIVE_WT_A" "$BASE" -q
git -C "$NATIVE_REPO" worktree add --detach "$NATIVE_WT_B" "$BASE" -q
echo "[demo] task A (backend) → $ROOT/wt/a | task B (frontend) → $ROOT/wt/b"

echo "// agent A" >> "$ROOT/wt/a/backend.go"
echo "export const B = 1" > "$ROOT/wt/b/frontend.tsx"
echo "[demo] separate files:"
echo "  A: $(git -C "$NATIVE_WT_A" status --porcelain | tr '\n' ' ')"
echo "  B: $(git -C "$NATIVE_WT_B" status --porcelain | tr '\n' ' ')"
echo "[demo] → no overlap, both proceed."

echo "// agent B" >> "$ROOT/wt/b/backend.go"
echo "[demo] B also touched backend.go → SAME_FILE conflict before integration."
echo "[demo] changeset A base=$BASE files=$(git -C "$NATIVE_WT_A" status --porcelain | wc -l)"
git -C "$NATIVE_REPO" worktree list
echo "[demo] scratch kept at $ROOT for inspection (remove when done)."
