# Historical development notes

These pre-release notes are historical, not current release claims. See
[Verification](VERIFICATION.md), [Installation](INSTALL.md), and [Security](../SECURITY.md)
for the release-candidate contract.

# Progress

## Completed
- Integration: approve → test-merge worktree → apply, commit, fast-forward
  canonical branch via update-ref, move recorded head; stale losers marked
  NEEDS_REBASE (applies) or CONFLICTED + conflict event; 409 on anything
  not APPROVED, 403 on runner tokens (agents can't merge). Proven live:
  merge moved head, sibling triaged same call, re-integrate refused.
- Absolute worktree paths (relative paths split git vs server cwd and
  broke diffs/builds/merges whenever repo != server dir); regression test.
- Newline-terminated diffs (git apply calls unterminated final lines
  corrupt); fixed in DiffBase.
- Dispatch: runner registry + heartbeats + per-runner queues; task assign
  creates workspace and enqueues; runner daemon polls, runs adapter in
  worktree, runs test command, reports; server marks COMPLETED/FAILED,
  auto-builds changeset on success, moves task to REVIEW (or BLOCKED on
  failure). Proven live: success and failure paths end to end.
- 204-empty-queue bug caught live and fixed (Poll treated 204 as error
  path and reported phantom work); regression test added.
- Persistence: services layer (projects/tasks/workspaces/changesets) over
  a Repo interface with memory + Postgres backends; server uses Postgres
  when DATABASE_URL is set, memory otherwise; worktree re-attach on boot;
  PG round-trip test (runs in CI) + Postgres service in CI workflow
- Live HTTP smoke: project → task → RUNNING → workspace → file edit →
  changeset → approve, all 200s
- Name: Ballast (Forge/Switchyard/Shipyard/SignalBox/Switchboard/Interlock/
  Roundhouse/Slipway/Bulwark all collide with live dev/agent tools)
- Initial commit aa9107e on main (56 files): full MVP vertical slice
- Repo scaffold: monorepo layout, go.mod, Docker, compose, CI, migrations
- Domain core: events bus, git worktrees, workspaces, tasks+leases,
  changesets, conflict V1, agent adapters (shell + codex + claude),
  integration pipeline, API (REST+SSE), runner daemon, CLI
- Web UI thin slice: project board, changeset review, activity feed (SSE)
- Tests: workspace isolation/cleanup, diff, changeset, conflict,
  lease overlap, full flow (2 worktrees → no-conflict → conflict →
  merge first → revalidate second)

## In Progress
- Service wiring: Postgres persistence behind api service interfaces
  in cmd/server; runner work dispatch (long-poll) + test execution

## Next
- Frontend: npm install + build (needs Node 18+; this machine has v10)
- NATS JetStream transport behind EventBus interface
- Temporal workflows for long agent runs + approval waits
- Preview environments, deployment + rollback, secrets broker
- Semantic conflict detection (symbols, API contracts, schema)

## Known Issues
- Frontend not yet built here (node on this machine is v10; needs 18+)
- OTEL exports to stdout only until collector wired
- Runner executes on control-plane host by default; remote runners
  use the same outbound-only protocol (polling, no inbound ports)

## Architecture Decisions
- Modular monolith first; NATS/Temporal/OpenFGA behind interfaces
- Git stays the source of execution truth; Postgres the source of
  coordination truth; workspaces are disposable
- Agents never merge; they produce changesets; humans approve;
  integration revalidates against new canonical state
- Conflicts are data (first-class objects), never silent
- Stdlib net/http routing + database/sql to minimize magic deps

## Deferred Work
- K8s, multi-repo projects, SSO, audit export, cost/token tracking,
  agent-to-agent messaging, auto-integration planning
