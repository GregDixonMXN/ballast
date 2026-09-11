package agent

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestShellUnavailable(t *testing.T) {
	a := NewShell("nope", "definitely-not-a-binary-xyz", nil)
	if a.Available(context.Background()) {
		t.Fatal("missing binary must be unavailable")
	}
}

func TestCmdRunsTaskBodyWithScopedEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix sh test")
	}
	a := Cmd(&DumbWrap{NoWrap: true})
	if !a.Available(context.Background()) {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	ex, err := a.StartTask(context.Background(), "task-1", dir, "echo $BALLAST_TASK_ID > out.txt")
	if err != nil {
		t.Fatal(err)
	}
	if ex.ExitCode != 0 {
		t.Fatalf("exit = %d (%s)", ex.ExitCode, ex.Stderr)
	}
	raw, err := os.ReadFile(dir + "/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "task-1" {
		t.Fatalf("scoped env missing, out.txt = %q", raw)
	}
}

func TestCmdWarnsWhenWrapMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix sh test")
	}
	a := Cmd(&DumbWrap{})
	if !a.Available(context.Background()) {
		t.Skip("no sh")
	}
	ex, err := a.StartTask(context.Background(), "t", t.TempDir(), "true")
	if err != nil {
		t.Fatal(err)
	}
	if ex.ExitCode != 0 {
		t.Fatalf("exit = %d (%s)", ex.ExitCode, ex.Stderr)
	}
	if !strings.Contains(ex.Stderr, "[ballast]") {
		t.Fatalf("expected wrap note on stderr, got %q", ex.Stderr)
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
