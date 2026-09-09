# Progress

## Completed
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
