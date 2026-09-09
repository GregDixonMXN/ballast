// Postgres round-trip test. Skips without DATABASE_URL (local dev uses
// memory). CI provides Postgres as a service, so every push exercises
// the real SQL: schema, upserts, JSON columns, NULL handling.
package store

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"

	"ballast/internal/changeset"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

func pgRepo(t *testing.T) *PGRepo {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL unset; postgres test runs in CI")
	}
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.SQL.Close() })
	if _, err := db.SQL.ExecContext(ctx, `TRUNCATE events, conflicts, changesets, leases,
workspaces, tasks, agent_instances, runners, projects, users, organizations`); err != nil {
		t.Fatalf("truncate: %v", err)
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
