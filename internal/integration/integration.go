// Package integration performs controlled merges. Flow: approve →
// revalidate changeset base against CURRENT canonical head → test-merge
// in a throwaway worktree → clean: apply, commit, fast-forward the
// canonical branch via update-ref, move the recorded head → dirty:
// mark NEEDS_REBASE or CONFLICTED with reason. Force-merge is not
// implemented on purpose. The canonical repo is never touched except
// through update-ref after an ancestry check.
package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

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

// Applies reports whether diff would apply cleanly onto base in repo,
// tested inside a throwaway detached worktree that is always removed.
func Applies(ctx context.Context, repo, base, diff, workRoot string) bool {
	if strings.TrimSpace(diff) == "" {
		return true
	}
	root := workRoot
	if root == "" {
		root = os.TempDir()
	}
	scratch, err := os.MkdirTemp(root, "merge-check-*")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	wt := filepath.Join(scratch, "wt")
	if err := git.AddWorktree(ctx, repo, wt, base); err != nil {
		return false
	}
	defer func() { _ = git.RemoveWorktree(ctx, repo, wt) }()
	f, err := os.CreateTemp("", "cs-*.diff")
	if err != nil {
		return false
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(diff); err != nil {
		return false
	}
	_ = f.Close()
	return git.ApplyCheck(ctx, wt, f.Name()) == nil
}

// Integrate merges cs into branch of repo. It mutates cs.Status to one
// of MERGED, NEEDS_REBASE, or CONFLICTED; the caller persists cs and,
// on success, records NewHead as the project's canonical head.
var integrationMu sync.Mutex

func Integrate(ctx context.Context, repo, branch string, cs *changeset.Changeset, workRoot string) (*Result, error) {
	integrationMu.Lock()
	defer integrationMu.Unlock()
	if err := git.ValidateBranch(ctx, repo, branch); err != nil {
		return nil, err
	}
	common, err := git.CommonDir(ctx, repo)
	if err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(common, "ballast-integration.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("integration already active: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	head, err := git.Head(ctx, repo, "refs/heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("read canonical head: %w", err)
	}
	if cs.Base != head {
		if !Applies(ctx, repo, head, cs.Diff, workRoot) {
			cs.Status = changeset.Conflicted
			return &Result{Conflict: true, Reason: "base moved and diff no longer applies to " + short(head)}, nil
		}
		// Base moved but the diff still applies onto head: fall through
		// and apply it there (rebase-apply). NEEDS_REBASE remains for
		// cases that need a fresh agent run, not as a dead end.
		cs.Base = head
	}
	if strings.TrimSpace(cs.Diff) == "" {
		cs.Status = changeset.Merged
		return &Result{Merged: true, NewHead: head, Reason: "empty changeset merges trivially"}, nil
	}
	root := workRoot
	if root == "" {
		root = os.TempDir()
	}
	scratch, err := os.MkdirTemp(root, "merge-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	wt := filepath.Join(scratch, "wt")
	if err := git.AddWorktree(ctx, repo, wt, head); err != nil {
		return nil, fmt.Errorf("scratch worktree: %w", err)
	}
	defer func() { _ = git.RemoveWorktree(ctx, repo, wt) }()
	f, err := os.CreateTemp("", "cs-*.diff")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(cs.Diff); err != nil {
		return nil, err
	}
	_ = f.Close()
	if err := git.Apply(ctx, wt, f.Name()); err != nil {
		cs.Status = changeset.Conflicted
		return &Result{Conflict: true, Reason: "diff failed to apply: " + err.Error()}, nil
	}
	newSHA, err := git.Checkpoint(ctx, wt, "integrate "+cs.ID+" (task "+cs.TaskID+")")
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	ok, err := git.IsAncestor(ctx, repo, head, newSHA)
	if err != nil || !ok {
		return nil, fmt.Errorf("not fast-forwardable: %w", err)
	}
	if err := git.AdvanceCanonical(ctx, repo, branch, head, newSHA); err != nil {
		return nil, fmt.Errorf("advance branch: %w", err)
	}
	cs.Status = changeset.Merged
	return &Result{Merged: true, NewHead: newSHA, Reason: "applied and fast-forwarded"}, nil
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
