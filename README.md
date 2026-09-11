# Ballast

**A local control room for parallel software work.**

Plan tasks, give each worker its own Git worktree, inspect the result, and
integrate reviewed changes into a canonical branch. Ballast keeps the task,
workspace, changeset, and runner visible in one operational dashboard.

This is the **1.0.0-rc.1 local release candidate**, not a hosted service.

## The workflow

1. Register a local Git repository and choose its canonical branch.
2. Create a task with a clear outcome.
3. Create an isolated workspace for manual work, or assign a connected runner.
4. Review the exact changed files and patch in a changeset.
5. Approve or reject; integration is a separate, explicit action.
6. Resolve stale bases and conflicts before retrying integration.

The dashboard includes project overview, task planning, runner availability,
workspace inspection, and changeset review. The API and CLI expose the same
local control plane. No cloud account is required for manual work.

## Start locally

Build prerequisites: **Go 1.27.1 or newer**, **Node.js 24**, npm, Git, Bash,
curl, and Python 3 on Linux. The release archive includes the built Go binaries and
standalone dashboard; it needs Node.js, Git, Bash, and curl but no compiler.

```bash
./scripts/build.sh
./scripts/start.sh
```

Open **http://127.0.0.1:3000**. The launcher prints the *path* to your private
operator credential, never its value. Open that local file yourself and use
the dashboard's connection form. Do not commit it or paste it into an issue.

The launcher binds both services to loopback and keeps local state under
`.ballast/`. It starts the API and dashboard; it does not start a worker or
execute a task automatically. Ctrl+C stops the services.

See [Installation](docs/INSTALL.md) for archive installation, runner setup,
ports, storage, and backup. [Security](SECURITY.md) explains the trust model.

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

No distribution license has been selected in this repository. Its owner must
choose licensing terms before redistributing a public release. No license or
commercial support entitlement is implied by the release-candidate label.

## Suite
Works alone. With Paldron (policy gate + sandbox) and Docket (one policy for both): https://github.com/GregDixonMXN/paldron, https://github.com/GregDixonMXN/docket
