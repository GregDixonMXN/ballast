// Package store is the Postgres coordination-truth layer plus a memory
// fallback for tests/dev. It applies migrations/*.sql on boot (embedded)
// and exposes typed record structs mirroring the schema.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"ballast/migrations"

	_ "github.com/lib/pq"
)

// DB wraps sql.DB with domain helpers.
type DB struct {
	SQL *sql.DB
}

// Open connects to Postgres (DATABASE_URL) and applies migrations.
func Open(ctx context.Context, dsn string) (*DB, error) {
	sqldb, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqldb.PingContext(ctx); err != nil {
		return nil, err
	}
	db := &DB{SQL: sqldb}
	if err := db.migrate(ctx); err != nil {
		return nil, err
	}
	return db, nil
}

func (d *DB) migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := migrations.FS.ReadFile(n)
		if err != nil {
			return err
		}
		for _, stmt := range strings.Split(string(b), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" || strings.HasPrefix(stmt, "--") {
				continue
			}
			if _, err := d.SQL.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration %s: %w", n, err)
			}
		}
	}
	return nil
}
