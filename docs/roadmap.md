# Roadmap: from MVP to the DevOps OS for human + AI teams

## Product vision

Ballast becomes the operating system for software teams made of humans
and AI agents: one place where work is planned, executed in isolation,
observed live, reviewed, integrated safely, and deployed with rollback.
The human stays the approver; agents stay the workers; the system owns
coordination truth so nobody can silently destroy anyone else's work.

The product wins when a team says: "we stopped thinking about branches,
worktrees, CI plumbing, and merge fear — we assign work and approve
outcomes."

## Where we are (shipped, proven live)

project → repository → tasks → isolated worktrees → agents run →
changes tracked → overlap warnings → changeset (base, files, diff,
tests) → approve/reject → controlled integrate → losers revalidated
(NEEDS_REBASE/CONFLICTED, never force-merged). Live-verified end to
end on HTTP: dispatch loop, success path (REVIEW + changeset),
failure path (BLOCKED, workspace kept), merge (head moved, sibling
triaged same call, re-integrate refused 409, agents rejected 403).

Stack: Go modular monolith, Next.js thin client, Postgres (memory
fallback for dev), Git worktrees, typed events + SSE, Docker/CI/docs.

## How we sequence

Vertical slices that demo end to end, reliability before scale, no
new distributed system until one box hurts. Every phase ends with:
tests green, demo script updated, docs updated, pushed to main.

## Phase 1 — Hardening (one box, production-honest)

Goal: a team can trust Ballast with a real repository.

- Postgres as the default path: compose boot with migrate, backup +
  restore documented and tested, connection pooling tuned.
- Auth real: OIDC login, per-runner scoped tokens (runners act only on
  their own work), per-workspace task credentials, secrets never in
  logs/events/diffs (audit this mechanically in CI).
- Audit log writes: approvals, merges, secret access, auth changes all
  land in audit_entries with actor + rationale. Audit export (JSONL).
- Runner resilience: reconnect + resume, stale-heartbeat eviction,
  orphaned-worktree GC, disk quotas per runner, failed workspaces
  retained with explicit destroy.
- API maturity: versioned routes (/v1), structured error codes, rate
  limits on auth + dispatch endpoints, request request-ids in logs.
- Observability: OpenTelemetry exporter to a real backend, dashboards
  for the golden signals (dispatch latency, integration success rate,
  conflict rate, agent task duration), alerting on failed merges.
- Load proof: 50 concurrent workspaces, 20 runners on one box without
  degradation; numbers recorded in docs.
- Frontend buildable everywhere: Node 20 toolchain pinned, web CI
  green, phone approval flow manually tested.

Exit: a second team (not us) runs Ballast for a week on a real repo
without data loss, silent conflicts, or unexplained failures.

## Phase 2 — Team scale (humans coordinate)

Goal: many humans, many projects, one control plane.

- Organizations, invitations, roles (viewer/developer/admin enforced
  everywhere, OpenFGA behind the Authorizer interface when the role
  matrix outgrows it).
- SSO (enterprise OIDC/SAML) for orgs that require it.
- Notifications: review requests and conflict alerts via webhooks +
  email; SSE stays the live channel, notifications the async one.
- Task board UX: filters, search, bulk assign, task templates with
  default scopes and test commands.
- Conflict panel UX: who overlaps whom, on what files/regions, one
  suggested action each; ack flow that records who accepted the risk.
- Changeset review UX: side-by-side diffs, test output inline,
  approve/reject with rationale required.
- Multi-project dashboard: health across projects, runner fleet view.

Exit: a 10-human, 30-agent team runs a sprint on Ballast as the only
coordination tool.

## Phase 3 — Execution scale (runners everywhere)

Goal: work runs anywhere, control stays central. Only when Phase 1
hardware limits are hit with numbers.

- NATS JetStream behind events.Bus (subjects per project, durable
  consumers per projection); in-process bus remains for single-box.
- Temporal for long agent runs, approval waits, crash recovery,
  deployment workflows. Workflow interfaces first, server second.
- Hosted runners: Docker-based pool, then Kubernetes when the pool
  needs autoscaling — not before.
- Remote runner hardening: mutual TLS, runner attestations, capability
  scheduling (GPU runners, test runners, deploy runners), artifact
  relay for runners behind NAT.
- Build/test caching shared across workspaces (content-addressed),
  so 100 agents don't compile the world 100 times.
- Multi-repository projects: one task spanning repos with linked
  changesets that integrate atomically or not at all.

Exit: 100+ concurrent agents across local + hosted + remote runners,
p95 dispatch-to-start under 30s.

## Phase 4 — Intelligence (semantic coordination)

Goal: catch conflicts that text overlap cannot see. Only on top of
Phase 3 volume — semantic analysis needs real conflict data to train
against, and we log every conflict from day one for exactly this.

- Symbol graph per repo (LSP/AST indexing): who defines, who calls.
- API contract tracking: endpoint + schema changes flagged against
  consumers in other workspaces before either merges.
- Database schema dependency tracking: migrations ordered, conflicts
  on the same tables/columns surfaced with the migration graph.
- Dependency graph: lockfile changes intersected across changesets.
- Semantic conflict prediction: same-file-clean but semantically
  coupled changes produce warnings with evidence, not blocks.
- Agent-to-agent messaging and negotiation: lease handoffs, merge
  order proposals, human-escalation protocol.
- Automatic integration planning: given N approved changesets, propose
  the merge order with lowest expected conflict cost.

Exit: measured reduction in post-merge breakages vs the syntactic
baseline, reported per project — no black boxes without numbers.

## Phase 5 — Delivery (the OS completes)

Goal: code flows to production through Ballast, and back.

- Preview environments per changeset (ephemeral, URL per changeset).
- Staging/production pipelines with approvals, progressive rollouts,
  and one-click rollback to any recorded head.
- Secrets broker: short-lived, scope-bound credentials minted per
  workspace and per deploy; full audit trail.
- Deployment records + production telemetry feedback: a deploy that
  regresses opens an investigation task with the offending changeset
  attached.
- Agent cost/token tracking per task, project, and provider; budgets
  with enforcement gates.
- Policy engine: org-defined rules (required reviewers, test gates,
  freeze windows) enforced at dispatch and integration time.
- Compliance: audit export, data retention controls, regional hosting
  notes.

Exit: a team ships production releases exclusively through Ballast
for a quarter, including at least one rollback.

## Deliberate non-goals

- No replacement for Git, no custom database, no custom containers.
- No Kubernetes before hosted runners need it; no microservices
  before team boundaries need them.
- No semantic layer before syntactic coordination is boring.
- No coupling to one AI provider, ever. New providers arrive as
  adapters, never core changes.
- No silent conflict resolution, ever. The system warns, proposes,
  and records — humans (or explicit policy) decide.
