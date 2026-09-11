# Ballast

**Git traffic cop for parallel humans and agents, on your own machine.**

Tasks → isolated Git worktrees → a dumb runner executes ONE command →
changeset (files/diff/tests) → overlap/rebase check → approve/integrate.
The dashboard on loopback views that state; it does not run your work.

This is the **1.0.0-rc.1 local release candidate**, not a hosted service.

## Start locally

Prerequisites: **Go 1.27.1+**, **Node.js 24**, Git, Bash, curl, Python 3 on Linux.

```bash
./scripts/build.sh
./scripts/start.sh
```

Open **http://127.0.0.1:3000**. The launcher prints the *path* to your private
operator credential, never its value. Open that local file yourself and use
the dashboard's connection form. Do not commit it or paste it into an issue.

`start.sh` launches the API and dashboard only. It never starts a worker,
never calls a model, never executes a task. Ctrl+C stops the services.

## Prove it with no model

```bash
./scripts/demo-green.sh  # one task, one file, end to end
./scripts/demo.sh        # two writers, one file: loser is marked, never force-merged
```

Both run against temp repos and temp server state with network keys scrubbed
from the environment. No API key, no agent CLI, no existing repo touched.
Exit 0 means the story holds.

## Bring your own agent as TASK_CMD

A task's `command` is the whole agent: a shell line, another CLI, anything.

```bash
# task 1
command: sh -c 'echo one >> src/a.txt'
# or: command: claude -p "add retry logic to src/net.py"
# or: command: codex exec "fix the failing test in web/"
```

The runner claims one READY task under a lease (409 if already claimed),
runs the command with cwd pinned to that task's worktree, enforces the
task timeout, then runs the task's `test_command` (per-task gate — a
project-wide suite other tasks break will deadlock you, so keep gates
scoped) and opens a changeset from the git diff. A model saying "tests
passed" is not evidence; exit codes are. Then it exits. No conversation,
no retries, no model inside Ballast's default path.

## Optional: record and gate with Annalist + Paldron

If the `annalist` and `paldron` binaries are on PATH, the runner wraps
each command automatically:

```text
annalist run -- paldron exec --policy <paldron.toml> -- <TASK_CMD>
```

Missing binaries run the raw command with a warning on stderr
(`--no-wrap` forces raw; `--paldron-policy` sets the policy file).
Links: https://github.com/GregDixonMXN/annalist,
https://github.com/GregDixonMXN/paldron

## BALLAST_BRAIN (default off)

The planner (outline → tasks), the supervise verifier (follow-up tasks),
and the in-process model tool-loop do not run unless `BALLAST_BRAIN=1`.
Default launches are facts only: assign, worktrees, gates, changesets,
overlap, integrate.

## What isolation means

Worktrees separate working directories, **not operating-system authority**.
Run only trusted commands and agents. A local process can access files and
Git administration allowed by its OS account; Ballast is not a sandbox.
The supported release topology is one control plane and same-host runners
sharing repository paths. It is not multi-tenant SaaS or a distributed runner
scheduler.

## Development and release

```bash
./scripts/check.sh        # Go formatting, vet, race tests, web type/build
./scripts/demo.sh         # synthetic Git workflow; no existing repo is modified
./scripts/package.sh     # binaries + standalone web + docs + checksums
```

- [Verification](docs/VERIFICATION.md): exact checks and known limitations.
- [Development](docs/development.md): toolchain and test fixtures.
- [Release guide](docs/RELEASE.md): reproduce and validate a package.
- [Changelog](CHANGELOG.md): release-candidate changes.
- [Contributing](CONTRIBUTING.md): contribution and review expectations.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/server`, `cmd/runner`, `cmd/ballast` | API, worker daemon, CLI |
| `internal/` | Auth, state, workspaces, changesets, integration |
| `apps/web/` | Responsive Next.js dashboard and fixed-origin API proxy |
| `scripts/` | Build, local launch, checks, packaging |
| `docs/` | Installation, operations, verification, architecture |

License: MIT — see [LICENSE](LICENSE).
