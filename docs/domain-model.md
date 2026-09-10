# Domain model

The local store or PostgreSQL holds coordination records. Git holds repository
objects, branch refs, and worktree state. Neither is a substitute for the other.

| Entity | Principal fields / behavior |
| --- | --- |
| Project | Local repository path, canonical branch, recorded SHA |
| Task | Project, title/description, scope hints, guarded lifecycle |
| Workspace | Project/task identity, absolute worktree path, base SHA, status |
| Changeset | Project/task/workspace result, exact patch, files, review status |
| Runner | Registered local worker, heartbeat/capabilities, process-local dispatch |
| Event | Project/actor/type/entity/timestamp and JSON metadata |

Task states are TODO, RUNNING, BLOCKED, REVIEW, DONE; transitions are constrained,
not an arbitrary linear chain. Review states include IN_REVIEW, APPROVED, REJECTED,
MERGED, NEEDS_REBASE, and CONFLICTED. A review approval does not advance a branch.

Failed/interrupted workspaces are retained for inspection. Git worktrees contain
working data and must be included with their repository in a backup. They are not
safe to discard merely because a coordination record exists.

The SQL schema also contains future-facing tables. Their presence does not imply
a working organization UI, deployment service, secrets broker, build-artifact
system, or compliance audit pipeline. See the [roadmap](roadmap.md).
