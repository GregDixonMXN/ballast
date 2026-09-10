// PostgreSQL tests opt in only to an explicitly named disposable database.
// Each test owns a fresh schema; no existing tables are truncated.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/lib/pq"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ballast/internal/changeset"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

func disposablePostgresURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "ballast_test") {
		return nil, fmt.Errorf("PostgreSQL tests require a postgres URL for a ballast_test* disposable database")
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		return nil, fmt.Errorf("PostgreSQL test database must be loopback-only")
	}
	return u, nil
}
func pgTestDB(t *testing.T) *DB {
	t.Helper()
	raw := os.Getenv("BALLAST_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("BALLAST_TEST_DATABASE_URL unset; use scripts/check-postgres.sh")
	}
	u, err := disposablePostgresURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.PingContext(ctx); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	var actual string
	if err = admin.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual); err != nil || !strings.HasPrefix(actual, "ballast_test") {
		admin.Close()
		t.Fatalf("not a disposable test database: %q %v", actual, err)
	}
	schema := "ballast_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(ctx, `CREATE SCHEMA `+pq.QuoteIdentifier(schema)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	conn, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		if _, err := admin.ExecContext(cleanCtx, `DROP SCHEMA `+pq.QuoteIdentifier(schema)+` CASCADE`); err != nil {
			t.Errorf("clean test schema: %v", err)
		}
		admin.Close()
	})
	return &DB{SQL: conn}
}
func pgRepo(t *testing.T) *PGRepo {
	t.Helper()
	db := pgTestDB(t)
	if err := db.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewPGRepo(db.SQL)
}

func TestPostgresRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := pgRepo(t)

	p := project.NewProject("", "pg-demo", "/tmp/x", "main")
	p.CanonicalSHA = "abc123"
	if err := r.SaveProject(ctx, p); err != nil {
		t.Fatalf("save project: %v", err)
	}
	got, err := r.GetProject(ctx, p.ID)
	if err != nil || got.Name != "pg-demo" || got.CanonicalSHA != "abc123" {
		t.Fatalf("get project = %+v %v", got, err)
	}
	if err := r.SetCanonicalHead(ctx, p.ID, "def456"); err != nil {
		t.Fatalf("set head: %v", err)
	}

	ta := task.New(p.ID, "Backend", "api", []string{"src/api/*", "src/auth/*"})
	ta.AssigneeID = ""
	if err := r.SaveTask(ctx, ta); err != nil {
		t.Fatalf("save task: %v", err)
	}
	lt, err := r.ListTasks(ctx, p.ID)
	if err != nil || len(lt) != 1 || len(lt[0].Scopes) != 2 {
		t.Fatalf("list tasks = %+v %v", lt, err)
	}

	w := workspace.Workspace{ID: uuid.NewString(), ProjectID: p.ID, TaskID: ta.ID,
		RepoPath: "/tmp/x", Base: "abc123", Path: "/tmp/w1", Status: workspace.Ready}
	if err := r.SaveWorkspace(ctx, w); err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	lw, err := r.ListWorkspaces(ctx, p.ID)
	if err != nil || len(lw) != 1 || lw[0].Status != workspace.Ready {
		t.Fatalf("list workspaces = %+v %v", lw, err)
	}

	cs := changeset.Changeset{ID: uuid.NewString(), ProjectID: p.ID, TaskID: ta.ID,
		Base: "abc123", Files: []string{"a.go", "b.go"}, Diff: "diff...",
		Status: changeset.InReview}
	if err := r.SaveChangeset(ctx, cs); err != nil {
		t.Fatalf("save changeset: %v", err)
	}
	gc, err := r.GetChangeset(ctx, cs.ID)
	if err != nil || len(gc.Files) != 2 || gc.Status != changeset.InReview {
		t.Fatalf("get changeset = %+v %v", gc, err)
	}
}
