# Domain model

Postgres is coordination truth; Git is execution truth. Worktrees are
disposable; history is not.

- Organization 1→* User, Project
- Project 1→1 Repository path + branch + canonical SHA (head moves only
  via controlled integration)
- Project 1→* Task (status TODO→RUNNING→BLOCKED→REVIEW→DONE, guarded
  transitions), Task 1→* Workspace over time, 1 active in MVP
- Workspace: task + agent + runner + repo + base commit + path + status
  (CREATING→READY→RUNNING→WAITING→COMPLETED→FAILED→CONFLICTED→DESTROYED).
  Failed workspaces persist until explicit destroy.
- WorkspaceLease: owner + path pattern + active window. Overlap warns.
- AgentInstance: provider (codex/claude/shell/custom) + label + active.
  Execution records: command argv, stdout/stderr, exit, timestamps.
- Changeset: task + agent + base + files + diff + test ref + status
  (DRAFT→IN_REVIEW→APPROVED/REJECTED→MERGED, or NEEDS_REBASE/CONFLICTED).
- Conflict: project + workspaces + kind (SAME_FILE/SAME_REGION/
  STALE_BASE/INTEGRATE_CONFLICT/SCOPE_OVERLAP now; SYMBOL/API_CONTRACT/
  SCHEMA/DEPENDENCY/SEMANTIC later) + severity + files + reason +
  action + status.
- Review/Approval: changeset + reviewer + decision + rationale.
- Build/TestRun: changeset + runner + command + result + logs ref.
- Event: id + project + actor (type+id) + type + entity + timestamp +
  metadata. Persisted; UI streams them.
- AuditEntry: like events, but only for sensitive actions (auth changes,
  approvals, merges, secret access, deploys).
- Environment/Deployment: skeletal until preview envs land (name, project,
  changeset, status, url, rollback target).

See `migrations/0001_init.sql` for the exact schema.
