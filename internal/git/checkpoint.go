package git

import (
	"context"
	"fmt"
)

// Checkpoint stages everything and commits with message, returning the SHA.
// Used for agent/test checkpoints inside a private worktree only.
func Checkpoint(ctx context.Context, dir, message string) (string, error) {
	if _, err := run(ctx, dir, "add", "-A"); err != nil {
		return "", fmt.Errorf("stage: %w", err)
	}
	if _, err := run(ctx, dir, "commit", "-m", message, "--allow-empty"); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return Head(ctx, dir, "HEAD")
}
