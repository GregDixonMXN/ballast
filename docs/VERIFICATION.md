# Release verification

Target: Ballast **1.0.0-rc.1**, Linux x86-64 local release candidate.
Verified 2026-09-10 by full release workflow; no package published
(publication needs owner licensing/signing/hosted-CI decisions).

Toolchain: system Go 1.23.3 too old for go.mod (needs >= 1.27.1), so
release scripts run with `GO=/usr/local/go/bin/go
GOTOOLCHAIN=go1.27.1` (cached toolchain, no download). `check.sh`
accepts a `GOTOOLCHAIN` override for this; default stays `local`.
Node 24 per package requirements.

## Verified

- `scripts/check.sh`: gofmt clean, `go vet`, `go test -race ./...`,
  web typecheck + production build, `git diff --check`. Two fixes
  applied: `GOTOOLCHAIN` override knob; two extra parens in
  `app/projects/[id]/page.tsx` workspaces/reviews ternaries.
- Live PostgreSQL (ephemeral `postgres:16-alpine` fixture via
  `check-postgres.sh`): migration bodies/versioning, rollback+retry,
  concurrent migrations, embedded-schema reopen, round-trip. All pass.
- `tests/release_smoke.py --bin-dir bin`: 28/28 (server workflow,
  restart persistence, exact patches, credential absent from logs).
- `tests/runner_smoke.py --bin-dir bin`: 15/15 (success/failure paths,
  timeout cancellation incl. descendant kill, SIGTERM cancel, restart
  blocks interrupted dispatch, no replay, token absent from logs).
- `scripts/package.sh --skip-build` +
  `scripts/check-container.sh`: archive verifies (`SHA256SUMS`),
  smoke passes against extracted `bin/`, packaged dashboard launches
  and renders (curl 200).
- Real browser (desktop): packaged build launched with isolated
  `BALLAST_STATE_DIR` — landing renders, operator-token auth unlocks
  the session, projects dashboard renders live counts (0 projects,
  0/0 runners). Server stopped after.

## Not verified

- Mobile viewport flows (no mobile browser available here).
- Hosted CI, signing, licensing, multi-tenant deployment (owner
  decisions, out of scope for a local candidate).

## Scope and known limits

- Trusted local Linux host, one control plane, same-path co-located runners.
- Worktrees do not sandbox the host or shared Git administration.
- Local snapshot state and Git changes are not one global transaction.
- Runner registrations/dispatch are process-local; interrupted work is blocked
  and retained for inspection, not automatically replayed exactly once.
- No provider request, external publication, signing, hosted CI, paid license,
  multi-tenant deployment, or independent security certification is claimed.
- Operator token/state, task text, source patches, and backups can be sensitive.

Only synthetic fixtures are used in release checks. Existing developer repositories,
actual operator credentials, and personal/production databases are not test inputs.
