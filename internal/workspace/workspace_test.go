package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// initRepo builds a tiny git repo with two files for isolation tests.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	must := func(name string, args ...string) {
		t.Helper()
		if out, err := gitHEAD(ctx, dir, name, args); err != nil {
			t.Fatalf("%s: %v (%s)", name, err, out)
		}
	}
	must("init", "init")
	must("config", "config", "user.email", "t@t.t")
	must("config", "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "backend.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "frontend.tsx"), []byte("export {}\n"), 0o644)
	must("add", "add", "-A")
	must("commit", "commit", "-m", "init")
	return dir
}

func TestCreateIsolated(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	m := NewManager(t.TempDir())
	a, err := m.Create(ctx, "p", "task-a", repo)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create(ctx, "p", "task-b", repo)
	if err != nil {
		t.Fatal(err)
	}
	if a.Path == b.Path {
		t.Fatal("workspaces share a path")
	}
	if a.Base == "" || a.Base != b.Base {
		t.Fatalf("expected same pinned base, got %q %q", a.Base, b.Base)
	}
	if a.Status != Ready || b.Status != Ready {
		t.Fatalf("expected READY, got %q %q", a.Status, b.Status)
	}
}

func TestChangedFilesAndDiff(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	m := NewManager(t.TempDir())
	w, err := m.Create(ctx, "p", "task-a", repo)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(w.Path, "backend.go"), []byte("package a\n// agent A\n"), 0o644)
	files, err := m.ChangedFiles(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "backend.go" {
		t.Fatalf("changed files = %v", files)
	}
	diff, err := m.Diff(ctx, w.ID)
	if err != nil || diff == "" {
		t.Fatalf("expected diff, got %q %v", diff, err)
	}
}

func TestDestroyCleansWorktree(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	m := NewManager(t.TempDir())
	w, err := m.Create(ctx, "p", "task-a", repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Destroy(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Path); !os.IsNotExist(err) {
		t.Fatal("worktree path still exists")
	}
	w, _ = m.Get(w.ID)
	if w.Status != Destroyed {
		t.Fatalf("status = %q", w.Status)
	}
}

func TestFailedPreservedUntilDestroy(t *testing.T) {
	m := NewManager(t.TempDir())
	w := &Workspace{ID: "x", Status: Failed}
	m.ws["x"] = w
	if _, err := m.SetStatus("x", Completed); err == nil {
		t.Fatal("failed workspace must not be relabeled completed")
	}
	// Failed is not auto-destroyed: still tracked.
	if _, ok := m.Get("x"); !ok {
		t.Fatal("failed workspace vanished")
	}
}

func TestPathsAbsolute(t *testing.T) {
	m := NewManager("relative-root")
	if !filepath.IsAbs(m.Root) {
		t.Fatalf("root = %q, want absolute", m.Root)
	}
}

func TestTerminalStatusCannotResume(t *testing.T) {
	m := NewManager(t.TempDir())
	m.Attach(&Workspace{ID: "terminal", Status: Completed})
	if _, err := m.SetStatus("terminal", Running); err == nil {
		t.Fatal("completed workspace resumed")
	}
	if _, err := m.SetStatus("terminal", Status("UNKNOWN")); err == nil {
		t.Fatal("invalid status accepted")
	}
}
