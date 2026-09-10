package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitHomeAndFilteredSecrets(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "synthetic-config"), []byte("configured"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BALLAST_TOKEN", "synthetic-secret")
	a := NewShell("fixture", "sh", []string{"-c"})
	a.Home = home
	result, err := a.StartTask(context.Background(), "task", t.TempDir(), `cat "$HOME/synthetic-config"; printf '|%s|%s' "$BALLAST_TOKEN" "$XDG_CONFIG_HOME"`)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout != "configured||"+filepath.Join(home, ".config") {
		t.Fatalf("unexpected execution: %+v", result)
	}
	if _, err := os.Stat(home); err != nil {
		t.Fatal("explicit home removed", err)
	}
}

func TestDefaultHomeDisposable(t *testing.T) {
	a := NewShell("fixture", "sh", []string{"-c"})
	result, err := a.StartTask(context.Background(), "task", t.TempDir(), `printf '%s' "$HOME"`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Stdout, "ballast-agent-home-") {
		t.Fatalf("unexpected home %q", result.Stdout)
	}
	if _, err := os.Stat(result.Stdout); !os.IsNotExist(err) {
		t.Fatalf("temporary home retained: %v", err)
	}
}
