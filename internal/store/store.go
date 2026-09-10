// Package store owns persistent coordination records and migrations.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"ballast/migrations"
	_ "github.com/lib/pq"
)

type DB struct{ SQL *sql.DB }

func Open(ctx context.Context, dsn string) (*DB, error) {
	sqldb, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err = sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	db := &DB{SQL: sqldb}
	if err = db.migrate(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	return db, nil
}
func (d *DB) migrate(ctx context.Context) error { return d.migrateFS(ctx, migrations.FS) }

// Execute complete SQL files, not semicolon-delimited fragments: comments,
// quoted strings and procedural bodies are all legal inside a migration.
// Version recording and schema changes commit together, serialized across
// concurrent server starts by a PostgreSQL transaction advisory lock.
func (d *DB) migrateFS(ctx context.Context, source fs.FS) error {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return err
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(638031228923105)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ballast_schema_migrations (name TEXT PRIMARY KEY, sha256 TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, name := range names {
		body, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		var prior string
		err = tx.QueryRowContext(ctx, `SELECT sha256 FROM ballast_schema_migrations WHERE name=$1`, name).Scan(&prior)
		if err == nil {
			if prior != checksum {
				return fmt.Errorf("migration %s changed after application", name)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ballast_schema_migrations(name,sha256) VALUES($1,$2)`, name, checksum); err != nil {
			return err
		}
	}
	return tx.Commit()
}
