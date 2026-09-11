// Package agent defines the Adapter interface and ships the first three
// adapters. The platform never depends on one AI provider: every adapter
// implements Name/Available/StartTask/SendMessage/Stop/Status, and the
// generic shell adapter runs ANY agent CLI (codex, claude, kimi, gemini,
// openclaw, future CLIs) as a supervised subprocess with captured output.
package agent

import (
	"ballast/internal/executil"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Execution captures everything about one agent run — never secrets.
type Execution struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	AgentID   string    `json:"agent_id"`
	Workspace string    `json:"workspace"`
	Command   []string  `json:"command"`
	Stdout    string    `json:"stdout"`
	Stderr    string    `json:"stderr"`
	ExitCode  int       `json:"exit_code"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

// Status of a live agent process.
type Status struct {
	Running   bool      `json:"running"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

// Adapter is the provider boundary. Small on purpose.
type Adapter interface {
	Name() string
	Available(ctx context.Context) bool
	StartTask(ctx context.Context, taskID, workspace string, prompt string) (*Execution, error)
	SendMessage(ctx context.Context, execID, message string) error
	Stop(ctx context.Context, execID string) error
	Status(ctx context.Context, execID string) (Status, error)
}

// ShellAdapter runs an arbitrary CLI with argv (no shell interpolation),
// cwd pinned to the workspace, and env allow-listed per task.
type ShellAdapter struct {
	name    string
	binary  string
	baseArg []string
	Env     []string // extra env, filtered before spawn
	Home    string   // explicit operator-selected agent config home; empty creates a disposable home
	// TaskEnv injects per-task scoped env (task id, worktree path, policy
	// paths). Values still pass through executil.Environment, so only
	// PATH/LANG/LC_ALL/BALLAST_* survive — never credentials.
	TaskEnv func(taskID, workspace string) []string
	// DumbWrap, when non-nil, wraps the command as
	//   annalist run -- paldron exec --policy P -- <binary...>
	// with feature detection: missing binaries run raw with a warning
	// on stderr. Only the "cmd" adapter sets this; codex/claude run bare.
	DumbWrap *DumbWrap

	mu    sync.Mutex
	procs map[string]*exec.Cmd
	pids  map[string]int
	start map[string]time.Time
}

// DumbWrap configures the record-and-gate wrapper around TASK_CMD.
type DumbWrap struct {
	// PaldronPolicy enables the paldron exec gate; empty skips paldron.
	PaldronPolicy string
	// NoWrap forces the raw command even when the binaries exist.
	NoWrap bool
}

// resolveWrap builds the argv prefix. Each element reports whether it
// applied, so the runner can warn instead of silently changing shape.
func (d *DumbWrap) resolveWrap() (prefix []string, notes []string) {
	if d == nil || d.NoWrap {
		return nil, nil
	}
	if _, err := exec.LookPath("annalist"); err == nil {
		prefix = append(prefix, "annalist", "run", "--")
		notes = append(notes, "annalist record on")
	} else {
		notes = append(notes, "annalist not found: running raw")
	}
	if d.PaldronPolicy != "" {
		if _, err := exec.LookPath("paldron"); err == nil {
			if st, err := os.Stat(d.PaldronPolicy); err == nil && !st.IsDir() {
				prefix = append(prefix, "paldron", "exec", "--policy", d.PaldronPolicy, "--")
				notes = append(notes, "paldron gate on")
			} else {
				notes = append(notes, "paldron policy unreadable: gate skipped")
			}
		} else {
			notes = append(notes, "paldron not found: gate skipped")
		}
	}
	return prefix, notes
}

// NewShell builds a generic command adapter: e.g. NewShell("codex",
// "codex", []string{"exec"}) or NewShell("custom", os.Getenv("AGENT_BIN"), nil).
func NewShell(name, binary string, baseArgs []string) *ShellAdapter {
	return &ShellAdapter{name: name, binary: binary, baseArg: baseArgs,
		procs: map[string]*exec.Cmd{}, pids: map[string]int{}, start: map[string]time.Time{}}
}

func (s *ShellAdapter) Name() string { return s.name }

// Available reports whether the binary resolves in PATH.
func (s *ShellAdapter) Available(_ context.Context) bool {
	_, err := exec.LookPath(s.binary)
	return err == nil
}

// StartTask spawns binary + baseArgs + prompt in workspace, capturing output.
// The process is supervised; cancellation stops it and records the result.
// With DumbWrap set (the "cmd" adapter), prompt is the TASK_CMD shell body
// run as `sh -c <prompt>`, optionally wrapped in annalist/paldron, with
// per-task scoped env from TaskEnv.
func (s *ShellAdapter) StartTask(ctx context.Context, taskID, workspace, prompt string) (*Execution, error) {
	argv := append([]string{s.binary}, s.baseArg...)
	argv = append(argv, prompt)
	var warnings string
	if s.DumbWrap != nil {
		prefix, notes := s.DumbWrap.resolveWrap()
		argv = append(prefix, argv...)
		if len(notes) > 0 {
			warnings = "[ballast] " + strings.Join(notes, "; ") + "\n"
		}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workspace
	executil.Configure(cmd)
	env := append([]string{}, s.Env...)
	if s.TaskEnv != nil {
		env = append(env, s.TaskEnv(taskID, workspace)...)
	}
	cmd.Env = executil.Environment(env)
	home := s.Home
	if home == "" {
		var err error
		home, err = os.MkdirTemp("", "ballast-agent-home-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(home)
	} else {
		var err error
		home, err = filepath.Abs(home)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(home)
		if err != nil {
			return nil, fmt.Errorf("agent home: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("agent home must be a directory")
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"))
	var so, se executil.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	id := uuid.NewString()
	started := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s start: %w: %s", s.name, err, se.String())
	}
	s.mu.Lock()
	s.procs[id] = cmd
	s.pids[id] = cmd.Process.Pid
	s.start[id] = started
	s.mu.Unlock()
	waitErr := cmd.Wait()
	ended := time.Now().UTC()
	code := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	s.mu.Lock()
	delete(s.procs, id)
	s.mu.Unlock()
	return &Execution{ID: id, TaskID: taskID, AgentID: s.name, Workspace: workspace,
		Command: argv, Stdout: so.String(), Stderr: warnings + se.String(),
		ExitCode: code, StartedAt: started, EndedAt: ended}, nil
}

func (s *ShellAdapter) SendMessage(_ context.Context, execID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.procs[execID]; !ok {
		return fmt.Errorf("no live execution %s", execID)
	}
	return fmt.Errorf("streaming input not supported in MVP; start a new task turn")
}

func (s *ShellAdapter) Stop(_ context.Context, execID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd, ok := s.procs[execID]
	if !ok {
		return fmt.Errorf("no live execution %s", execID)
	}
	return executil.Kill(cmd)
}

func (s *ShellAdapter) Status(_ context.Context, execID string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pid, ok := s.pids[execID]; ok {
		if _, live := s.procs[execID]; live {
			return Status{Running: true, PID: pid, StartedAt: s.start[execID]}, nil
		}
		return Status{Running: false, PID: pid, StartedAt: s.start[execID]}, nil
	}
	return Status{}, fmt.Errorf("unknown execution %s", execID)
}

// Cmd returns the dumb command adapter: `sh -c <TASK_CMD>` in the
// worktree. Bring your own agent as TASK_CMD — a shell line, another
// CLI, anything. No model, no conversation, no retries: run, gate, exit.
func Cmd(wrap *DumbWrap) *ShellAdapter {
	a := NewShell("cmd", "sh", []string{"-c"})
	a.DumbWrap = wrap
	a.TaskEnv = func(taskID, workspace string) []string {
		return []string{"BALLAST_TASK_ID=" + taskID, "BALLAST_WORKSPACE=" + workspace}
	}
	return a
}

// Codex returns the preferred first adapter: `codex exec <prompt>`.
func Codex() *ShellAdapter { return NewShell("codex", "codex", []string{"exec"}) }

// Claude returns the second adapter: `claude -p <prompt>` (print mode).
func Claude() *ShellAdapter { return NewShell("claude", "claude", []string{"-p"}) }

// Registry picks the first available adapter (codex → claude → custom).
func Registry(custom *ShellAdapter) []Adapter {
	out := []Adapter{Codex(), Claude()}
	if custom != nil {
		out = append(out, custom)
	}
	return out
}
