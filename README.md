# Ballast — DevOps OS for human + AI software teams (MVP)

Smallest excellent MVP proving: **multiple humans and AI agents can safely
work on the same repository concurrently without unknowingly conflicting
with or destroying one another's work.**

## What this is

Modular Go monolith (control plane + runner + CLI), Next.js web client,
PostgreSQL as source of truth, Git worktrees for isolation, events for
everything, conflict detection before integration.

## Quickstart

Prerequisites: Go 1.23+, Node 18+, Git, Postgres (or Docker).

```bash
./scripts/dev.sh        # postgres + migrate + api + runner + web
./scripts/demo.sh       # demo repo, two tasks, two worktrees, conflict check
go test ./...           # unit + integration (temp git repos, no DB needed for git/conflict/changeset)
```

## Layout

- `cmd/server` — control-plane API (REST + SSE events)
- `cmd/runner` — execution-node daemon (worktrees, agents, tests)
- `cmd/ballast` — CLI client of the API
- `internal/` — domain packages (project, task, workspace, agent, git,
  changeset, integration, conflict, events, auth, telemetry, store, api, lease)
- `apps/web` — Next.js operational UI (thin client of the API)
- `migrations/` — Postgres schema, `store` applies them on boot
- `docs/` — architecture, domain model, events, runner, security,
  development, roadmap, progress

## MVP workflow

project → repository → task ×2 → isolated worktrees → agents run →
changed files tracked → overlap warning → changeset (base, files, diff,
tests) → approve/reject → controlled integrate → revalidate next
changeset (NEEDS_REBASE/CONFLICTED, never silent force-merge).

## Docs

Start at `docs/architecture.md`, then `docs/development.md`.
Progress log: `docs/progress.md`.
