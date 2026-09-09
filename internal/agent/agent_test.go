package agent

import (
	"context"
	"runtime"
	"testing"
)

func TestShellUnavailable(t *testing.T) {
	a := NewShell("nope", "definitely-not-a-binary-xyz", nil)
	if a.Available(context.Background()) {
		t.Fatal("missing binary must be unavailable")
	}
}

func TestShellEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix echo test")
	}
	a := NewShell("echo", "echo", nil)
	if !a.Available(context.Background()) {
		t.Skip("no echo")
	}
	ex, err := a.StartTask(context.Background(), "t", t.TempDir(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if ex.ExitCode != 0 {
		t.Fatalf("exit = %d (%s)", ex.ExitCode, ex.Stderr)
	}
	st, err := a.Status(context.Background(), ex.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Running {
		t.Fatal("finished execution must not report running")
	}
}
