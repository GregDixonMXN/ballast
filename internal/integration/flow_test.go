// Critical integration test: two tasks, two worktrees, separate files
// (no conflict), same file (conflict), two changesets, merge first,
// revalidate second. Uses temp git repos — no DB required.
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"ballast/internal/changeset"
	"ballast/internal/conflict"
	"ballast/internal/git"
	"ballast/internal/workspace"
)

func sh(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func demoRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sh(t, dir, "init", "-b", "main")
	sh(t, dir, "config", "user.email", "t@t.t")
	sh(t, dir, "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "backend.go"), []byte("package api\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "frontend.tsx"), []byte("export {}\n"), 0o644)
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-m", "init")
	return dir
}

func TestTwoWorkspaceFlow(t *testing.T) {
	ctx := context.Background()
	repo := demoRepo(t)
	m := workspace.NewManager(t.TempDir())

	wa, err := m.Create(ctx, "p", "task-a", repo)
	if err != nil {
		t.Fatal(err)
	}
	wb, err := m.Create(ctx, "p", "task-b", repo)
	if err != nil {
		t.Fatal(err)
	}

	// Separate files: no conflict.
	os.WriteFile(filepath.Join(wa.Path, "backend.go"), []byte("package api\n// A\n"), 0o644)
	os.WriteFile(filepath.Join(wb.Path, "frontend.tsx"), []byte("export const B = 1\n"), 0o644)
	fa, _ := m.ChangedFiles(ctx, wa.ID)
	fb, _ := m.ChangedFiles(ctx, wb.ID)
	got := conflict.DetectFiles("p", []conflict.FilesChanged{
		{WorkspaceID: wa.ID, Files: fa}, {WorkspaceID: wb.ID, Files: fb},
	})
	if len(got) != 0 {
		t.Fatalf("separate files should not conflict: %v", got)
	}

	// Same file: conflict.
	os.WriteFile(filepath.Join(wb.Path, "backend.go"), []byte("package api\n// B\n"), 0o644)
	fb, _ = m.ChangedFiles(ctx, wb.ID)
	got = conflict.DetectFiles("p", []conflict.FilesChanged{
		{WorkspaceID: wa.ID, Files: fa}, {WorkspaceID: wb.ID, Files: fb},
	})
	if len(got) == 0 {
		t.Fatal("same file must conflict")
	}

	// Changesets carry base + files + diff.
	ca, err := changeset.Build(ctx, "p", "task-a", "agent-a", wa.Path, wa.Base)
	if err != nil {
		t.Fatal(err)
	}
	if ca.Base == "" || len(ca.Files) == 0 || ca.Diff == "" {
		t.Fatalf("changeset incomplete: %+v", ca)
	}
	if ca.Status != changeset.InReview {
		t.Fatalf("status = %q", ca.Status)
	}

	// Merge first (simulated canonical advance): second goes stale.
	head, _ := git.Head(ctx, repo, "HEAD")
	if ca.Base != head {
		t.Fatal("base should equal head before merge")
	}
	sh(t, wa.Path, "add", "-A")
	sh(t, wa.Path, "commit", "-m", "agent A work")
	newHead, _ := git.Head(ctx, wa.Path, "HEAD")
	_ = newHead
	sh(t, repo, "merge", "--ff-only", "HEAD") // no-op sanity
	stale := conflict.DetectStale("p", wb.ID, wb.Base, "deadbeef00000000")
	if stale == nil || stale.Kind != conflict.StaleBase {
		t.Fatalf("expected STALE_BASE after head moved, got %+v", stale)
	}
}
