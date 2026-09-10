package executil

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentAndBoundedOutput(t *testing.T) {
	t.Setenv("BALLAST_TOKEN", "synthetic-secret")
	cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "env; head -c 400000 /dev/zero")
	Configure(cmd)
	var out Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "synthetic-secret") || strings.Contains(s, "BALLAST_TOKEN") {
		t.Fatal("inherited credential")
	}
	if len(s) > OutputLimit+32 || !strings.HasSuffix(s, "[output truncated]") {
		t.Fatal("output not bounded")
	}
}
func TestCancellationKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30 & wait")
	Configure(cmd)
	var out Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	started := time.Now()
	if err := cmd.Run(); err == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("child group survived cancellation")
	}
}
