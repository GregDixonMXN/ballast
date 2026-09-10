package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func TestPostgresFixtureGuard(t *testing.T) {
	for _, raw := range []string{"postgres://127.0.0.1/production", "postgres://example.com/ballast_test", "host=127.0.0.1 dbname=ballast_test", "https://127.0.0.1/ballast_test"} {
		if _, err := disposablePostgresURL(raw); err == nil {
			t.Errorf("unsafe fixture URL accepted: %q", raw)
		}
	}
	if _, err := disposablePostgresURL("postgres://127.0.0.1:5432/ballast_test_run"); err != nil {
		t.Fatal(err)
	}
}
func TestPostgresMigrationBodiesAndVersioning(t *testing.T) {
	db := pgTestDB(t)
	ctx := context.Background()
	source := fstest.MapFS{"0001_test.sql": {Data: []byte("-- A leading comment must not suppress the next statement.\nCREATE TABLE body_test(value TEXT);\nDO $$ BEGIN INSERT INTO body_test(value) VALUES ('inside;body'); END $$;\n")}}
	if err := db.migrateFS(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateFS(ctx, source); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM body_test WHERE value='inside;body'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM ballast_schema_migrations`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	source["0001_test.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE changed_history(value TEXT);")}
	if err := db.migrateFS(ctx, source); err == nil || !strings.Contains(err.Error(), "changed after application") {
		t.Fatal("modified applied migration accepted", err)
	}
}
func TestPostgresMigrationRollbackAndRetry(t *testing.T) {
	db := pgTestDB(t)
	ctx := context.Background()
	source := fstest.MapFS{"0001_test.sql": {Data: []byte("CREATE TABLE rollback_test(value TEXT); INSERT INTO rollback_test VALUES ('before failure'); SELECT definitely_missing_function();")}}
	if err := db.migrateFS(ctx, source); err == nil {
		t.Fatal("expected migration failure")
	}
	var absent bool
	if err := db.SQL.QueryRowContext(ctx, `SELECT to_regclass('rollback_test') IS NULL AND to_regclass('ballast_schema_migrations') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("partial schema/version survived", absent, err)
	}
	source["0001_test.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE rollback_test(value TEXT); INSERT INTO rollback_test VALUES ('retry');")}
	if err := db.migrateFS(ctx, source); err != nil {
		t.Fatal("retry after rollback", err)
	}
}
func TestPostgresConcurrentMigrations(t *testing.T) {
	db := pgTestDB(t)
	source := fstest.MapFS{"0001_test.sql": {Data: []byte("CREATE TABLE concurrent_migration(value TEXT); INSERT INTO concurrent_migration VALUES ('once');")}}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := db.migrateFS(context.Background(), source); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count int
	if err := db.SQL.QueryRow(`SELECT count(*) FROM concurrent_migration`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestPostgresEmbeddedSchemaReopen(t *testing.T) {
	db := pgTestDB(t)
	ctx := context.Background()
	if err := db.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"organizations", "users", "projects", "runners", "agent_instances", "tasks", "workspaces", "leases", "changesets", "conflicts", "events", "audit_entries"} {
		var exists bool
		if err := db.SQL.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil || !exists {
			t.Errorf("schema table %s missing: %v", table, err)
		}
	}
}
