# Development

Use Go 1.27.1, Node.js 24, npm, Git, Bash, curl, and Python 3. The optional
PostgreSQL test fixture uses Docker. Build dependencies are not runtime credentials.

```bash
./scripts/check.sh
./scripts/build.sh
python3 tests/release_smoke.py --bin-dir bin
./scripts/check-postgres.sh
```

`GO=/path/to/go` selects the compiler in build/check scripts. `check.sh` rejects
unformatted Go, runs vet and race tests, and checks/builds the frontend. It unsets
`DATABASE_URL`; PostgreSQL tests require a dedicated `BALLAST_TEST_DATABASE_URL`.
`check-postgres.sh` creates and removes its own ephemeral loopback PostgreSQL
container without mounting any user data. Never use a personal database for tests.

`release_smoke.py` starts the actual API binary with temporary HOME, state, and
Git repositories. It verifies authorization, persistent review/restart, committed
and binary files, exact path/whitespace preservation, approval gating, a dirty
canonical tree, integration consistency, and credential-free logs.

The dashboard uses a same-origin `/api` proxy to a fixed `BALLAST_API_URL`.
No browser-supplied URL is used to choose the upstream. The server reads private
credentials from a file; the web connection form holds its user-entered token in
memory. Keep tokens out of URLs, logs, fixtures, and public client environment.

Core invariants:

- Project/task/workspace membership must match.
- The selected branch, not whichever branch happens to be checked out, is the base.
- A review patch preserves actual file bytes, names, and committed worker changes.
- Test failures or missing required test results do not become successful reviews.
- Approval and integration are separate; stale/conflicted work is not force-merged.
- Failed state writes do not appear as successful durable state.
- Tests never use a real developer repository, database, credential, or worktree.

See `docs/VERIFICATION.md` for actual executed checks; a command being documented
does not imply it has passed on every platform.
