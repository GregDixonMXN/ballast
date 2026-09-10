# PostgreSQL

Default local storage requires no database server. For PostgreSQL, set
`DATABASE_URL` in the server's private runtime environment before starting it.
This selects an independent PostgreSQL store; it does not migrate local JSON data.

For an isolated **development-only** database:

```bash
docker compose -f docker/compose.yml up -d postgres
DATABASE_URL='postgres://ballast_dev@127.0.0.1:15432/ballast_dev?sslmode=disable' \
  ./scripts/start.sh
```

This fixture uses loopback-only trust authentication. Do not deploy it on a
shared/public host. A real database needs its own account, restricted network,
TLS, private credentials, backup policy, and access controls. Do not put a real
connection credential in a tracked file, shell history, URL pasted into chat,
or frontend environment variable.

Schema migrations are embedded in the server. Back up before upgrading. Run only
one Ballast control plane for this release; PostgreSQL does not make its in-memory
runner dispatch distributed or fault-tolerant.

Backups must include both PostgreSQL (`pg_dump` through your normal protected
credential setup) and the repositories/worktrees at the same quiescent point.
Restoring SQL without Git objects and worktrees does not restore a deployment.
State directories also contain the operator credential and need private backups.

For regression tests, run `./scripts/check-postgres.sh`. It creates a separate
throwaway database and selects it with `BALLAST_TEST_DATABASE_URL`. Never reuse
your development or production database as a test fixture.
