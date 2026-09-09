# Architecture

## Why this shape

The product risk is coordination, not distribution. So: one deployable
control plane (modular monolith), runners as dumb-safe executors, Postgres
as coordination truth, Git as execution truth, events as the nervous
system. NATS, Temporal, OpenFGA, K8s all sit behind interfaces we define
now and adopt when load demands — not before.

## Components

- Control plane (`cmd/server` + `internal/api`): owns projects, tasks,
  workspaces, changesets, conflicts, reviews, approvals, integrations.
  REST for state, SSE for live events. No agent code runs here.
- Runner (`cmd/runner`): registers with control plane, heartbeats,
  polls for assigned work (outbound-only, no inbound ports), manages
  worktrees under its root, spawns agent processes with captured
  stdout/stderr/exit/timestamps, runs tests, reports file changes.
  MVP runs co-located; protocol already supports remote nodes.
- Store (`internal/store`): Postgres via database/sql; applies
  `migrations/*.sql` on boot; memory implementation for tests.
- Event bus (`internal/events`): typed events, persisted to Postgres,
  fanned out in-process to SSE subscribers. Interface-compatible with
  a future NATS JetStream transport (subject-per-project, durable
  consumer per projection).
- Agent adapters (`internal/agent`): `Adapter` interface
  (Name/Available/StartTask/SendMessage/Stop/Status). Shell adapter runs
  any CLI (codex, claude, custom) with env + cwd sandboxing; Codex and
  Claude adapters are thin profiles over it. Execution records carry
  command metadata, never secrets (secrets are env-injected at spawn
  and redacted in logs/events).
- Workspace manager (`internal/workspace` over `internal/git`):
  CREATING→READY→RUNNING→WAITING→COMPLETED→FAILED→CONFLICTED→DESTROYED.
  Failed workspaces are kept for forensics. Checkpoints = commits.
- Conflict V1 (`internal/conflict`): same-file overlap, same-region
  overlap (hunk paths), stale base (workspace base != canonical head),
  git integration conflicts (test-merge), lease scope overlap.
  Conflicts are rows, with severity + suggested action.
- Integration (`internal/integration`): approve → revalidate base →
  test-merge into throwaway worktree → on clean: fast-forward/merge
  commit to canonical branch, update head, emit events; on dirty:
  mark NEEDS_REBASE or CONFLICTED, never force.

## Data flow (vertical slice)

API request → task → workspace (worktree at base commit) → agent
execution (captured) → file changes → changeset (diff + tests) →
review/approve → conflict revalidation → integration → canonical head
moves → dependents revalidated. Every arrow emits a typed event with
org/project/task/workspace/agent/runner/trace IDs in structured logs.

## Security

Agents are untrusted: separate platform/repo/task/deploy credentials,
scoped tokens minted per workspace, secrets never logged, args
validated (no shell string building — argv only), dangerous actions
behind approval gates, audit log for sensitive operations.

## Observability

Structured slog with IDs on every record; counters/timings for
workspace_create_duration, agent_task_duration, process failures,
active agents/workspaces, conflicts, changesets, integration and test
outcomes. Trace IDs propagate API→runner→git→tests→integration.
OTEL SDK wiring is isolated in `internal/telemetry` (stdout for MVP).
