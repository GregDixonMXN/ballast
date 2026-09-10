package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func ValidateBranch(ctx context.Context, repo, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch")
	}
	_, err := run(ctx, repo, "check-ref-format", "refs/heads/"+branch)
	return err
}

// AdvanceCanonical uses compare-and-swap for bare/unattached branches and a
// clean fast-forward for checked-out branches, preserving index/file truth.
// A repository lock in integration serializes Ballast operations. Independent
// human Git writes must not run concurrently with integration.
func AdvanceCanonical(ctx context.Context, repo, branch, old, next string) error {
	ref := "refs/heads/" + branch
	out, err := run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	path := ""
	checkout := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path = strings.TrimPrefix(line, "worktree ")
		}
		if line == "branch "+ref {
			checkout = path
		}
	}
	if checkout != "" {
		state, err := run(ctx, checkout, "status", "--porcelain=v1", "--untracked-files=all")
		if err != nil {
			return err
		}
		if state != "" {
			return fmt.Errorf("canonical worktree is dirty; commit or stash changes before integration")
		}
		current, err := Head(ctx, checkout, "HEAD")
		if err != nil {
			return err
		}
		if current != old {
			return fmt.Errorf("canonical head changed")
		}
		// --ff-only invokes Git's ref transaction with the expected old head and
		// updates the checked-out files/index together. Disable repository hooks.
		_, err = run(ctx, checkout, "-c", "core.hooksPath=/dev/null", "merge", "--ff-only", "--no-edit", next)
		return err
	}
	_, err = run(ctx, repo, "update-ref", ref, next, old)
	return err
}

func CommonDir(ctx context.Context, repo string) (string, error) {
	s, err := run(ctx, repo, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(s) {
		s = filepath.Join(repo, s)
	}
	return filepath.Abs(s)
}
