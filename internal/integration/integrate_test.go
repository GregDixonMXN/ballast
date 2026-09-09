package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ballast/internal/changeset"
	"ballast/internal/git"
)

func mergeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sh(t, dir, "init", "-b", "main")
	sh(t, dir, "config", "user.email", "t@t.t")
	sh(t, dir, "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644)
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-m", "init")
	return dir
}

// worktreeWithEdits creates a detached worktree and writes files,
// returning the worktree path and base HEAD.
func worktreeWithEdits(t *testing.T, repo string, files map[string]string) (string, string) {
	t.Helper()
	ctx := context.Background()
	base, err := git.Head(ctx, repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	sh(t, repo, "worktree", "add", "--detach", wt, base)
	for name, content := range files {
		os.WriteFile(filepath.Join(wt, name), []byte(content), 0o644)
	}
	return wt, base
}

func TestIntegrateMergesAndMovesHead(t *testing.T) {
	ctx := context.Background()
	repo := mergeRepo(t)
	wt, base := worktreeWithEdits(t, repo, map[string]string{"a.go": "package a\n// A\n"})
	cs, err := changeset.Build(ctx, "p", "ta", "agent", wt, base)
	if err != nil {
		t.Fatal(err)
	}
	cs.Status = changeset.Approved
	res, err := Integrate(ctx, repo, "main", cs, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Merged || res.NewHead == "" || res.NewHead == base {
		t.Fatalf("expected merge with new head, got %+v", res)
	}
	if cs.Status != changeset.Merged {
		t.Fatalf("status = %q", cs.Status)
	}
	head, _ := git.Head(ctx, repo, "main")
	if head != res.NewHead {
		t.Fatalf("branch head %q != %q", head, res.NewHead)
	}
	// Read through the branch pointer: the main checkout's files stay
	// stale after update-ref by design (worktrees are never touched).
	// run() trims trailing whitespace, so expect the trimmed blob.
	content, err := git.Show(ctx, repo, "main:a.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "package a\n// A" {
		t.Fatalf("canonical content = %q", content)
	}
}

func TestIntegrateStaleCleanNeedsRebase(t *testing.T) {
	ctx := context.Background()
	repo := mergeRepo(t)
	// Loser built on the old base, touching b.go.
	wtLose, base := worktreeWithEdits(t, repo, map[string]string{"b.go": "package b\n// loser\n"})
	lose, err := changeset.Build(ctx, "p", "tb", "agent", wtLose, base)
	if err != nil {
		t.Fatal(err)
	}
	// Winner touches a.go and merges first, moving the head.
	wtWin, _ := worktreeWithEdits(t, repo, map[string]string{"a.go": "package a\n// winner\n"})
	win, _ := changeset.Build(ctx, "p", "ta", "agent", wtWin, base)
	win.Status = changeset.Approved
	if res, err := Integrate(ctx, repo, "main", win, t.TempDir()); err != nil || !res.Merged {
		t.Fatalf("winner: %+v %v", res, err)
	}
	// Loser revalidated: base moved, diff still applies elsewhere.
	lose.Status = changeset.Approved
	res, err := Integrate(ctx, repo, "main", lose, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged || lose.Status != changeset.NeedsRebase {
		t.Fatalf("expected NEEDS_REBASE, got %+v status %q", res, lose.Status)
	}
}

func TestIntegrateStaleConflicted(t *testing.T) {
	ctx := context.Background()
	repo := mergeRepo(t)
	// Both touch the same line of a.go.
	wtLose, base := worktreeWithEdits(t, repo, map[string]string{"a.go": "package a\n// loser\n"})
	lose, _ := changeset.Build(ctx, "p", "tb", "agent", wtLose, base)
	wtWin, _ := worktreeWithEdits(t, repo, map[string]string{"a.go": "package a\n// winner\n"})
	win, _ := changeset.Build(ctx, "p", "ta", "agent", wtWin, base)
	win.Status = changeset.Approved
	if res, err := Integrate(ctx, repo, "main", win, t.TempDir()); err != nil || !res.Merged {
		t.Fatalf("winner: %+v %v", res, err)
	}
	lose.Status = changeset.Approved
	res, err := Integrate(ctx, repo, "main", lose, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged || !res.Conflict || lose.Status != changeset.Conflicted {
		t.Fatalf("expected CONFLICTED, got %+v status %q", res, lose.Status)
	}
}

func TestIntegrateRequiresApproved(t *testing.T) {
	// Integrate itself is status-agnostic (the API gates APPROVED);
	// an empty diff on matching base still merges trivially.
	ctx := context.Background()
	repo := mergeRepo(t)
	base, _ := git.Head(ctx, repo, "HEAD")
	cs := &changeset.Changeset{ID: "x", Base: base, Status: changeset.Approved}
	res, err := Integrate(ctx, repo, "main", cs, t.TempDir())
	if err != nil || !res.Merged {
		t.Fatalf("empty: %+v %v", res, err)
	}
}
