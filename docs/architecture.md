# Architecture

Ballast is a local modular Go control plane with a Next.js operational dashboard.
It separates **coordination state** (projects/tasks/reviews), **Git state**
(repository objects, refs, index, worktrees), and **execution** (runner processes).

## Components

- `cmd/server`: authenticated REST API, activity history and SSE, domain services,
  state recovery, dispatch, and controlled Git integration.
- `cmd/runner`: registers using a private operator credential, then uses a scoped
  runner token to poll and report assigned work. Executes trusted local adapters
  and test commands with bounded output and cancellation.
- `cmd/ballast`: private token-file CLI for resource operations and API requests.
- `apps/web`: responsive dashboard; an in-memory user token reaches a fixed local
  API through a same-origin server proxy. No operator token is embedded in JS.
- `internal/store`: atomic local JSON snapshots under a lifetime process lock by
  default; optional PostgreSQL records and versioned schema migrations.
- `internal/workspace`: creates detached Git worktrees pinned to the selected
  canonical branch and reattaches retained workspaces after restart.
- `internal/changeset`, `internal/git`: capture a reviewable patch relative to the
  workspace base, including committed edits, untracked files, binary data, and
  exact filenames without modifying the user's Git index.
- `internal/integration`: validates the base, applies to a scratch worktree, then
  advances a canonical branch under a repository lock. Clean checked-out branches
  are fast-forwarded with their index/files; unattached branches use a ref CAS.

## Invariants and boundaries

A task belongs to a project; its workspace and review must retain that identity.
Approval is separate from integration. A stale base becomes NEEDS_REBASE or
CONFLICTED rather than silently forcing a merge. Dirty canonical worktrees remain
untouched. Independent external Git writes must be quiescent during integration.

The state store and Git do not share one global transaction. Keep consistent
backups and inspect interrupted operations after a crash. Runner dispatch and
registrations are process-local; restart blocks interrupted work and does not
promise exactly-once replay. The supported topology is one server with same-host
runners and identical absolute paths, not distributed or multi-tenant execution.

Worktrees do **not** sandbox host or Git administrative authority. The product is
for trusted local commands. See [Security](../SECURITY.md) and [Installation](INSTALL.md).

## Request flow

Project → task → detached workspace → local work/runner → changeset → review →
approval → explicit integration → canonical head → sibling revalidation.

Activity records are operational history, not a complete transactional compliance
audit. They may include sensitive task text and file metadata. [Events](events.md)
describes the actual interfaces; roadmap types are not implemented services.
