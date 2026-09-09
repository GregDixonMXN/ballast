package git

import (
	"context"
	"fmt"
	"strings"
)

// ApplyCheck runs `git apply --check` for a diff file inside dir.
func ApplyCheck(ctx context.Context, dir, diffFile string) error {
	if _, err := run(ctx, dir, "apply", "--check", diffFile); err != nil {
		return fmt.Errorf("apply --check: %w", err)
	}
	return nil
}

// Apply applies a diff file inside dir for real.
func Apply(ctx context.Context, dir, diffFile string) error {
	if _, err := run(ctx, dir, "apply", diffFile); err != nil {
		return fmt.Errorf("git apply: %w", err)
	}
	return nil
}

// IsAncestor reports whether a is an ancestor of b in repo.
func IsAncestor(ctx context.Context, repo, a, b string) (bool, error) {
	_, err := run(ctx, repo, "merge-base", "--is-ancestor", a, b)
	if err != nil {
		if strings.Contains(err.Error(), "exit status 1") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// UpdateRef moves ref to sha without touching any working tree.
func UpdateRef(ctx context.Context, repo, ref, sha string) error {
	_, err := run(ctx, repo, "update-ref", ref, sha)
	return err
}
