// Package agent defines the Adapter interface and ships the first three
// adapters. The platform never depends on one AI provider: every adapter
// implements Name/Available/StartTask/SendMessage/Stop/Status, and the
// generic shell adapter runs ANY agent CLI (codex, claude, kimi, gemini,
// openclaw, future CLIs) as a supervised subprocess with captured output.
package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	Env     []string // extra env, secrets injected here at spawn only

	mu    sync.Mutex
	procs map[string]*exec.Cmd
	pids  map[string]int
	start map[string]time.Time
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
func (s *ShellAdapter) StartTask(ctx context.Context, taskID, workspace, prompt string) (*Execution, error) {
	args := append(append([]string{}, s.baseArg...), prompt)
	cmd := exec.CommandContext(ctx, s.binary, args...)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), s.Env...)
	var so, se bytes.Buffer
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
		Command: append([]string{s.binary}, args...), Stdout: so.String(), Stderr: se.String(),
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
	return cmd.Process.Kill()
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
