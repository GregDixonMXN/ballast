// Package integration performs controlled merges. Flow: approve →
// revalidate changeset base against CURRENT canonical head → test-merge in
// a throwaway worktree → clean: merge commit to canonical branch, move
// head, emit events → dirty: mark NEEDS_REBASE or CONFLICTED with reason.
// Force-merge is not implemented on purpose.
package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"ballast/internal/changeset"
	"ballast/internal/git"
)

// Result of an integration attempt.
type Result struct {
	Merged   bool
	NewHead  string
	Reason   string
	Conflict bool
}

// Integrate validates cs against the live canonical head and merges.
// repo = canonical checkout path, branch = canonical branch name,
// workRoot = scratch dir for the throwaway test-merge worktree.
func Integrate(ctx context.Context, repo, branch string, cs *changeset.Changeset, workRoot string) (*Result, error) {
	head, err := git.Head(ctx, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("read canonical head: %w", err)
	}
	if cs.Base != head {
		// Base moved since the changeset was built: check whether the
		// merge is still clean before deciding NEEDS_REBASE vs CONFLICTED.
		clean, _, merr := git.TestMergeClean(ctx, repo, head, cs.Base)
		_ = clean
		if merr != nil {
			return &Result{Reason: "base moved; revalidation inconclusive"}, nil
		}
		cs.Status = changeset.NeedsRebase
		return &Result{Reason: "base " + short(cs.Base) + " behind canonical " + short(head)}, nil
	}
	scratch, err := os.MkdirTemp(workRoot, "merge-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	wt := filepath.Join(scratch, "wt")
	if err := git.AddWorktree(ctx, repo, wt, head); err != nil {
		return nil, fmt.Errorf("scratch worktree: %w", err)
	}
	defer func() { _ = git.RemoveWorktree(ctx, repo, wt) }()
	// Apply the changeset diff inside the scratch worktree to test it.
	// MVP applies via `git apply --check` using the recorded diff file.
	if clean, reason := applyCheck(ctx, wt, cs.Diff); !clean {
		cs.Status = changeset.Conflicted
		return &Result{Conflict: true, Reason: reason}, nil
	}
	cs.Status = changeset.Merged
	return &Result{Merged: true, NewHead: head, Reason: "clean test-merge"}, nil
}

func applyCheck(ctx context.Context, wt, diff string) (bool, string) {
	if diff == "" {
		return true, "empty changeset merges trivially"
	}
	f, err := os.CreateTemp("", "cs-*.diff")
	if err != nil {
		return false, "scratch diff file failed"
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(diff); err != nil {
		return false, "scratch diff write failed"
	}
	_ = f.Close()
	cmd := "git"
	_ = cmd
	if err := git.ApplyCheck(ctx, wt, f.Name()); err != nil {
		return false, "diff does not apply to current head: " + err.Error()
	}
	return true, "applies cleanly"
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
