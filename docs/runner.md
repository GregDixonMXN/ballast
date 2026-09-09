# Runner protocol

Outbound-only: runners open connections to the control plane; the
control plane never dials into dev machines.

- Register: POST /runners {hostname, os, arch, cpu, memory, docker,
  git, capabilities} → {id, token}. Token scopes the runner to its own
  heartbeats and assigned work.
- Heartbeat: POST /runners/{id}/heartbeat every 15s (online/offline
  derived server-side from freshness).
- Work poll: GET /runners/{id}/work (long-poll in later milestones) →
  [{workspace_id, task, prompt, adapter, env_keys}]. Runner creates the
  worktree, spawns the adapter, streams stdout/stderr chunks as
  agent.message events, and posts file.changed summaries.
- Capabilities (MVP): cpu, memory, docker, git. Later: gpu, builds,
  tests, deploys, local inference.

Execution rules: one workspace per directory, argv-only process spawn
(no shell strings), secrets as env at spawn (never in events/logs),
cancellation on task reassign, failed workspaces kept for forensics.
