# Security

Agents are untrusted workers; humans approve; the system integrates.

- Credential tiers (never mixed): platform, repository, task, deploy.
  Runners get a scoped token; workspaces get task-scoped env; deploy
  credentials never reach agent processes.
- Secrets: env-injected at spawn, redacted from Execution records,
  events, and logs. Never in diffs, URLs, or error strings.
- Process safety: argv-only spawn (no `sh -c`), validated args,
  cwd pinned to the worktree, cancellation on reassign/stop.
- Auth: bearer tokens in dev, OIDC in prod (same header shape);
  `Authorizer` interface today, OpenFGA tuples tomorrow.
- Dangerous actions (approve, merge, deploy, secret read) need
  developer+ role and emit audit entries.
- Git safety: detached worktrees per worker, checkpoints are commits
  (no history rewrites), integration test-merges in scratch worktrees,
  never force-push canonical branches.
- Failure posture: conflicts and failed merges are recorded loudly
  (NEEDS_REBASE/CONFLICTED); nothing auto-resolves or hides.
