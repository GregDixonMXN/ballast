# Install and operate Ballast

## Supported release target

Linux x86-64, a single trusted local operator, and same-host runners. Use Node.js
24, Git, Bash, and curl. Source builds additionally require Go 1.27.1 and npm; packaging/checks use Python 3.
No hosted account, Docker daemon, or database server is required for the default
local storage mode.

## From a release archive

Keep the complete extracted directory together: the dashboard's Node modules
are part of the standalone distribution.

```bash
sha256sum -c ballast-1.0.0-rc.1-linux-x86_64.tar.gz.sha256
tar -xzf ballast-1.0.0-rc.1-linux-x86_64.tar.gz
cd ballast-1.0.0-rc.1-linux-x86_64
./scripts/start.sh
```

Open http://127.0.0.1:3000. Use the connection form to load the private
`operator.token` file from the location printed by the launcher. Its content is
not logged. Refreshing/disconnecting the dashboard forgets the in-memory token.

## From source

```bash
./scripts/build.sh
./scripts/start.sh
```

`GO=/absolute/path/to/go ./scripts/build.sh` selects a specific compiler.
Dependencies are installed from `apps/web/package-lock.json` using `npm ci`.

## Location and ports

```bash
BALLAST_STATE_DIR="$HOME/.local/share/ballast" \
BALLAST_API_PORT=8080 BALLAST_WEB_PORT=3000 ./scripts/start.sh
```

The launch script binds both HTTP services to `127.0.0.1`. It starts the API
and dashboard only. Ctrl+C stops both. It does not open ports to other machines
or execute an agent task automatically.

The state directory contains `state.json`, its process lock, `operator.token`,
and `worktrees/`. Keep repository and worktree locations stable: Git worktrees
and persisted records use absolute paths. Do not move the install directory
while using its default `.ballast/` data location; choose an external stable
`BALLAST_STATE_DIR` first.

If running services separately, the API accepts `--addr`, `--data`,
`--token-file`, and `--worktrees`. The standalone web server reads
`BALLAST_API_URL`, `HOSTNAME`, and `PORT`. Keep its upstream fixed to the local
API; do not forward operator tokens to an untrusted URL.

## Register a repository

Use the dashboard's new-project form with an absolute repository path and an
existing local canonical branch (typically `main`). The repository must have
at least one commit and a configured Git author identity. Start with a disposable
repository to learn the review/integration flow.

Manual workflow: create a task and workspace, make changes inside that workspace,
submit a changeset, inspect the diff, approve, then integrate. Approval alone does
not change the canonical branch. Commit or stash ordinary changes in a checked-out
canonical working tree before integrating.

## Start a runner

Runners execute trusted commands with your OS user's permissions. Install the
agent binary you want to use and keep it on PATH, or select a custom binary.
The API and runner must see identical absolute paths.

```bash
./bin/ballast-runner \
  --server http://127.0.0.1:8080 \
  --token-file "$HOME/.local/share/ballast/operator.token" \
  --worktrees "$HOME/.local/share/ballast/worktrees" \
  --name local-worker
```

Use a path matching your actual `BALLAST_STATE_DIR`. The operator credential is
used for registration; subsequent worker requests use a runner-scoped token.
By default, adapters receive a disposable unauthenticated home directory. For a
provider CLI that stores login/configuration on disk, configure a **dedicated**
private agent home using that tool's own login flow, then add
`--agent-home /absolute/path/to/dedicated-agent-home`. This deliberately grants
agents read/write access to that directory. Do not point it at a home containing
unrelated administrator or personal secrets. Ballast does not perform provider
login or copy credentials for you.

Keep the runner process open while assigning tasks. Runners are not durable remote
workers: after a control-plane restart, stop and re-register them and inspect
interrupted work before retrying. Failed workspaces are retained for inspection.

## CLI

```bash
./bin/ballast --token-file /path/to/operator.token projects
./bin/ballast --token-file /path/to/operator.token runners
./bin/ballast --token-file /path/to/operator.token activity
./bin/ballast --token-file /path/to/operator.token request GET /projects
```

The `request` command accepts a JSON argument or `-` to read JSON from stdin.
Use stdin for task text that should not appear in shell history. Do not pass
credentials as command-line arguments or embed them in URLs.

## Back up and restore

1. Stop runners, the API, the dashboard, and other writers to the managed repos.
2. Back up the **entire state directory and all managed repositories together**,
   preserving absolute paths, permissions, Git metadata, and worktrees.
3. Protect the backup: it contains an administrator token and may contain private
   source, task text, and diffs. No encryption is added by Ballast.
4. Restore as one consistent set at the original paths, then start the API.
5. Inspect projects, retained workspaces, and interrupted tasks before starting
   runners or integrating more work.

Do not copy only `state.json` and expect worktrees or repository objects to return.
For a local migration, stop the services and make a full backup before moving
files. Cross-path relocation and multi-server deployments are not supported.

## Optional PostgreSQL

See [PostgreSQL operations](postgres.md). The local JSON store is the default.
Setting `DATABASE_URL` selects PostgreSQL; it does not import existing local JSON
records. Never point a development/test command at a real deployment database.
