package workspace

import (
	"context"
	"os/exec"
)

// gitHEAD runs git -C dir args... returning combined output.
func gitHEAD(ctx context.Context, dir, _ string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
