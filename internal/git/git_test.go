package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchRepo creates a repo with one committed file and returns dir + base SHA.
func scratchRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	g("init", "-qb", "main")
	if err := os.WriteFile(filepath.Join(dir, "keep.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g("add", "-A")
	g("commit", "-qm", "base")
	out, err := run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return dir, out
}

func TestGeneratedJunkExcluded(t *testing.T) {
	dir, base := scratchRepo(t)
	ctx := context.Background()

	// Simulate an agent run: real change + interpreter junk.
	if err := os.MkdirAll(filepath.Join(dir, "__pycache__"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "__pycache__", "keep.cpython-312.pyc"), []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".pytest_cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pytest_cache", "CACHEDIR.TAG"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.py"), []byte("x = 1\ny = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := ChangedBase(ctx, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "keep.py" {
		t.Fatalf("ChangedBase = %v, want [keep.py]", files)
	}

	diff, err := DiffBase(ctx, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "keep.py") {
		t.Fatalf("DiffBase missing keep.py:\n%s", diff)
	}
	for _, junk := range []string{"pycache", ".pyc", "pytest_cache"} {
		if strings.Contains(diff, junk) {
			t.Fatalf("DiffBase leaks junk %q:\n%s", junk, diff)
		}
	}

	hunks, err := Hunks(ctx, dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 1 {
		t.Fatalf("Hunks keys = %v, want only [keep.py]", hunks)
	}
	if _, ok := hunks["keep.py"]; !ok {
		t.Fatalf("Hunks keys = %v, want [keep.py]", hunks)
	}
}
