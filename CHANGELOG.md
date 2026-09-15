# Changelog

## 1.0.0

First stable release. Everything in 1.0.0-rc.1, plus:

- Go module path `github.com/GregDixonMXN/ballast` with pinned toolchain,
  so plain builds and `go install` work.
- Validated end to end: full check suite, Postgres store checks, both
  no-model demos (green path and overlap-never-merges), release smoke
  against packaged binaries.

Support intent: single-host local operation; worktrees separate working
directories, not OS authority (pair with Paldron for sandboxing). Not
multi-tenant SaaS or a distributed scheduler. See docs/VERIFICATION.md
for limits.

## 1.0.0-rc.1 — local release candidate

- Operational dashboard for projects, tasks, runners, workspaces, and reviews.
- Private persistent operator credential, scoped runner authorization, and
  authenticated dashboard requests.
- Durable single-host local state and restart handling.
- Git branch, patch fidelity, ownership, and integration safeguards.
- Working local setup, CLI, build checks, and packaged standalone dashboard.
- Explicit execution, deployment, recovery, and licensing boundaries.

See `docs/VERIFICATION.md` for the exact verified revision and release limits.
This candidate has not been published, signed, or deployed as a hosted service.
