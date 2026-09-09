// Vertical slice through the services layer on a memory repo: project →
// tasks → transitions → isolated workspaces → edits → changed files →
// changeset → approve. Mirrors the API flow without HTTP or Postgres.
package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"ballast/internal/changeset"
	"ballast/internal/project"
	"ballast/internal/store"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("init", "-b", "main")
	g("config", "user.email", "t@t.t")
	g("config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "backend.go"), []byte("package api\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "frontend.tsx"), []byte("export {}\n"), 0o644)
	g("add", "-A")
	g("commit", "-m", "init")
	return dir
}

func mustID(t *testing.T, v any) string {
	t.Helper()
	switch x := v.(type) {
	case project.Project:
		return x.ID
	case task.Task:
		return x.ID
	case *workspace.Workspace:
		return x.ID
	case *changeset.Changeset:
		return x.ID
	case changeset.Changeset:
		return x.ID
	}
	t.Fatalf("cannot extract id from %T", v)
	return ""
}

func TestServicesVerticalSlice(t *testing.T) {
	repo := store.NewMemoryRepo()
	mgr := workspace.NewManager(t.TempDir())
	ps, ts, ws, cs := &Projects{Repo: repo}, &Tasks{Repo: repo},
		&Workspaces{Repo: repo, Mgr: mgr}, &Changesets{Repo: repo}

	p, err := ps.Create("demo", gitRepo(t), "main")
	if err != nil {
		t.Fatal(err)
	}
	projectID := mustID(t, p)

	ta, err := ts.Create(projectID, "Backend", "api work", []string{"src/api/*"})
	if err != nil {
		t.Fatal(err)
	}
	tb, err := ts.Create(projectID, "Frontend", "ui work", []string{"web/*"})
	if err != nil {
		t.Fatal(err)
	}
	taskA, taskB := mustID(t, ta), mustID(t, tb)

	if _, err := ts.Transition(taskA, "RUNNING"); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Transition(taskA, "DONE"); err == nil {
		t.Fatal("RUNNING → DONE must be rejected")
	}

	wa, err := ws.Create(projectID, taskA, "")
	if err != nil {
		t.Fatal(err)
	}
	wb, err := ws.Create(projectID, taskB, "")
	if err != nil {
		t.Fatal(err)
	}
	widA, widB := mustID(t, wa), mustID(t, wb)
	if widA == widB {
		t.Fatal("workspaces must be isolated")
	}

	// Simulate agent edits in each worktree.
	ctx := context.Background()
	wrecA, _ := repo.GetWorkspace(ctx, widA)
	wrecB, _ := repo.GetWorkspace(ctx, widB)
	os.WriteFile(filepath.Join(wrecA.Path, "backend.go"), []byte("package api\n// A\n"), 0o644)
	os.WriteFile(filepath.Join(wrecB.Path, "frontend.tsx"), []byte("export const B=1\n"), 0o644)

	fa, err := ws.ChangedFiles(widA)
	if err != nil || len(fa) != 1 || fa[0] != "backend.go" {
		t.Fatalf("A files = %v %v", fa, err)
	}

	built, err := cs.Build(projectID, taskA, "agent-a", widA)
	if err != nil {
		t.Fatal(err)
	}
	c := built.(*changeset.Changeset)
	if c.Base == "" || len(c.Files) == 0 || c.Diff == "" {
		t.Fatalf("changeset incomplete: %+v", c)
	}

	decided, err := cs.Decide(c.ID, "approve", "human")
	if err != nil {
		t.Fatal(err)
	}
	if decided.(changeset.Changeset).Status != changeset.Approved {
		t.Fatalf("expected APPROVED, got %+v", decided)
	}

	list, err := ts.List(projectID)
	if err != nil || len(list) != 2 {
		t.Fatalf("tasks = %v %v", list, err)
	}
}
