// Package executil bounds local subprocesses. This is supervision, not a
// filesystem/network sandbox: commands still run as the trusted local user.
package executil

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const OutputLimit = 256 << 10

// Buffer accepts all writes while retaining at most OutputLimit bytes.
type Buffer struct {
	mu        sync.Mutex
	b         bytes.Buffer
	truncated bool
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := OutputLimit - b.b.Len()
	if len(p) > left {
		p = p[:left]
		b.truncated = true
	}
	b.b.Write(p)
	return n, nil
}
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.b.String()
	if b.truncated {
		s += "\n[output truncated]"
	}
	return s
}

// Environment intentionally excludes inherited credentials, proxy settings,
// Git/SSH configuration, loader hooks and the operator's home directory.
func Environment(extra []string) []string {
	out := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	for _, v := range extra {
		key, _, ok := strings.Cut(v, "=")
		if ok && (key == "PATH" || key == "LANG" || key == "LC_ALL") {
			out = append(out, v)
		}
	}
	return out
}
func Configure(cmd *exec.Cmd) {
	cmd.Env = Environment(nil)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
