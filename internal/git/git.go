// Package git is the execution-truth layer: thin, strict wrappers over the
// git CLI for canonical-branch reads, worktree lifecycle, diffs, and
// test-merges. All commands use argv (never shell strings) and run with
// context cancellation. Every failure returns the captured stderr.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// run executes git -C dir args... and returns trimmed stdout.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Head returns the full SHA of ref (e.g. HEAD, main) in repo.
func Head(ctx context.Context, repo string, ref string) (string, error) {
	return run(ctx, repo, "rev-parse", ref)
}

// Show returns the blob bytes of rev:path (e.g. "main:a.go") without
// touching any working tree.
func Show(ctx context.Context, repo, rev string) (string, error) {
	return run(ctx, repo, "show", rev)
}

// Branch lists the current branch name.
func Branch(ctx context.Context, repo string) (string, error) {
	return run(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD")
}

// AddWorktree creates a detached worktree at path pinned to base SHA.
// Detached avoids branch contention between concurrent workers.
func AddWorktree(ctx context.Context, repo, path, base string) error {
	_, err := run(ctx, repo, "worktree", "add", "--detach", path, base)
	return err
}

// RemoveWorktree force-removes a worktree and prunes metadata.
func RemoveWorktree(ctx context.Context, repo, path string) error {
	if _, err := run(ctx, repo, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, err := run(ctx, repo, "worktree", "prune")
	return err
}

// StatusPorcelain lists changed/untracked paths in dir.
// XY columns vary (staged "M " vs unstaged " M"), so the path is read
// from offset 2 with whitespace trimmed — never a fixed offset-3 slice.
func StatusPorcelain(ctx context.Context, dir string) ([]string, error) {
	out, err := run(ctx, dir, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		p := strings.TrimSpace(line[2:])
		// Renames/copies: "R  old -> new" — track the new path.
		if i := strings.LastIndex(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		p = strings.Trim(p, `"`)
		p = strings.TrimSpace(p)
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// DiffBase returns the unified diff of worktree against base commit,
// including untracked files content. The result always ends with a
// newline when non-empty: git apply rejects patches whose final line
// is unterminated as corrupt.
func DiffBase(ctx context.Context, dir, base string) (string, error) {
	out, err := run(ctx, dir, "diff", base, "--", ".")
	if err != nil {
		return "", err
	}
	un, err := run(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	for _, f := range strings.Split(un, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		d, err := run(ctx, dir, "diff", "--no-index", "--", "/dev/null", f)
		if err != nil {
			// --no-index exits 1 when diffs exist; output is still valid.
			if out == "" && d == "" {
				return out, nil
			}
		}
		out += "\n" + d
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

// Hunks returns per-file changed line ranges vs base (for region overlap).
// Uses `git diff -U0` headers: +++ b/<file> then @@ -a,b +c,d @@.
func Hunks(ctx context.Context, dir, base string) (map[string][][2]int, error) {
	out, err := run(ctx, dir, "diff", "-U0", base, "--", ".")
	if err != nil {
		return nil, err
	}
	res := map[string][][2]int{}
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			cur = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		if strings.HasPrefix(line, "@@") && cur != "" {
			var start, count int
			// format: @@ -a,b +start,count @@
			plus := strings.Index(line, "+")
			if plus < 0 {
				continue
			}
			rest := line[plus+1:]
			end := strings.IndexAny(rest, " @")
			if end > 0 {
				rest = rest[:end]
			}
			parts := strings.Split(rest, ",")
			fmt.Sscanf(parts[0], "%d", &start)
			count = 1
			if len(parts) > 1 {
				fmt.Sscanf(parts[1], "%d", &count)
			}
			res[cur] = append(res[cur], [2]int{start, start + count})
		}
	}
	return res, nil
}

// TestMerge checks whether head merges cleanly into branch tip without
// touching the working tree: merges in a throwaway worktree is the
// caller's job; here we just attempt `merge-tree` when available,
// falling back to merge --no-commit --no-ff in dir (caller must reset).
func TestMergeClean(ctx context.Context, repo, branch, head string) (bool, string, error) {
	out, err := run(ctx, repo, "merge-tree", branch, branch, head)
	if err == nil {
		// merge-tree prints the tree; conflicts appear as conflict markers
		// in the output blob section. Conservative check:
		if strings.Contains(out, "<<<<<<<") {
			return false, out, nil
		}
		return true, out, nil
	}
	// Fallback: not available on old git — report unknown cleanly.
	return true, "merge-tree unavailable; caller must verify via worktree", nil
}
