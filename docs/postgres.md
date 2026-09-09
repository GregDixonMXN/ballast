# Postgres status (for Hermes, across sessions)

## Situation

The server has two modes, chosen at boot in cmd/server:

- DATABASE_URL set → Postgres backend (persistent). Migrations run
  automatically via internal/store.
- DATABASE_URL unset → memory backend (ephemeral). Logs which mode.

## Windows dev PC (this machine)

Local Postgres does NOT work here. History so you don't retry blindly:

1. No Docker, no Postgres installed. Installed both Go and gh via
   winget fine; `PostgreSQL.PostgreSQL.16` installed BROKEN (bin/ with
   no lib/ — initdb died on missing `$libdir/dict_snowball`).
2. `winget repair` fixed the missing libs; initdb + pg_ctl then booted
   and accepted connections for ~4 minutes before child processes
   crashed with 0xC0000142 (session/DLL teardown in this environment).
3. A pre-existing system Postgres service listens on 5432 but is
   password-locked (psql hangs on auth); the password is unrecoverable.
4. Native Windows git cannot resolve MSYS /tmp or /c/... paths — always
   pass C:/... paths (via `cygpath -m`) to git/psql/initdb/pg_ctl.

Verdict: do NOT attempt local Postgres on this PC again unless the
environment changes (Docker installed, or a fresh PG install + service
with known credentials). Memory mode is the local default here.

## What Postgres needs (Linux machine)

- Postgres 16 (docker compose postgres service, or native) with
  db/user/password `ballast`, reachable at
  `postgres://ballast:***@localhost:5432/ballast?sslmode=disable`.
- `export DATABASE_URL=<that>` then `go test -count=1 ./...` — the
  round-trip test in internal/store runs instead of skipping.
- Persistence proof: boot server, create project, kill, reboot, project
  still listed.

## This Linux machine (dev desktop, verified 2026-09-09)

- A system Postgres already listens on 127.0.0.1:5432 (password-locked,
  not ours). Do NOT fight it: our container maps host port 5433.
- Container `ballast-pg` (postgres:16-alpine, named volume not used —
  recreate with the same command if missing):
  `docker run -d --name ballast-pg -e POSTGRES_USER=ballast
  -e POSTGRES_PASSWORD=ballast -e POSTGRES_DB=ballast
  -p 127.0.0.1:5433:5432 postgres:16-alpine`
- `export DATABASE_URL='postgres://ballast:ballast@localhost:5433/ballast?sslmode=disable'`
- Go: system go is 1.22.2, module needs 1.23 (and toolchain download
  fails). Go 1.23.3 tarball kept at `~/go1.23.3.linux-amd64.tar.gz`;
  extract and prepend its `go/bin` to PATH.
- Fixed 2026-09-09: `TestPostgresRoundTrip` used hardcoded IDs `"w1"`/`"c1"`
  but id columns are UUID. The app generates real UUIDs everywhere
  (google/uuid), so the test was wrong, not the schema — it now uses
  `uuid.NewString()`. Full `go test ./...` green against the container.
- Persistence proof done here: created project `persist-demo` via the API,
  killed the server, rebooted, `GET /projects/{id}` returned it.

## CI

.github/workflows/ci.yml runs a postgres:16-alpine service with the
same credentials and DATABASE_URL, so every push exercises the real
SQL even though this PC cannot.
