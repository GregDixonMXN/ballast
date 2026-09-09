# Development

## Prerequisites

Go 1.23+, Node 20+, Git, Postgres 16 (or Docker).

## Boot

```bash
./scripts/dev.sh     # compose postgres, build, api :8080, runner, web :3000
```

Dev token prints at server boot; save as `BALLAST_TOKEN`. Web stores it
in localStorage; CLI takes `--token` / `$BALLAST_TOKEN`.

## Tests

```bash
go test ./...        # git/conflict/changeset use temp repos, no DB
```

Critical path test: `internal/integration/flow_test.go` — two
worktrees, separate-file clean, same-file conflict, changeset shape,
merge-first then stale detection.

## Demo without the API

```bash
bash scripts/demo.sh
```

## Repo rules

- `gofmt -l .` clean, `go vet ./...` clean, CI green before merge.
- Migrations are append-only (`migrations/NNNN_*.sql`).
- Events are append-only types; never rename, only add.
- Small interfaces, explicit errors, context on every blocking call.
