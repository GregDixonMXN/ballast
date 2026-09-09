package git

import (
	"context"
	"fmt"
)

// ApplyCheck runs `git apply --check` for a diff file inside dir.
func ApplyCheck(ctx context.Context, dir, diffFile string) error {
	if _, err := run(ctx, dir, "apply", "--check", diffFile); err != nil {
		return fmt.Errorf("apply --check: %w", err)
	}
	return nil
}
