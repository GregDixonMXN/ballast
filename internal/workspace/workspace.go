// Package workspace manages isolated Git worktrees bound to tasks.
// No two workers ever share a mutable directory: one workspace = one
// worktree path = one task assignment. Failed workspaces are preserved
// for forensics; only explicit destroy removes them.
package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"ballast/internal/git"
	"github.com/google/uuid"
)

// Status of the workspace lifecycle.
type Status string

const (
	Creating   Status = "CREATING"
	Ready      Status = "READY"
	Running    Status = "RUNNING"
	Waiting    Status = "WAITING"
	Completed  Status = "COMPLETED"
	Failed     Status = "FAILED"
	Conflicted Status = "CONFLICTED"
	Destroyed  Status = "DESTROYED"
)

// Workspace binds task + agent + repo + base commit + path.
type Workspace struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	TaskID    string    `json:"task_id"`
	AgentID   string    `json:"agent_id,omitempty"`
	RunnerID  string    `json:"runner_id,omitempty"`
	RepoPath  string    `json:"repo_path"`
	Base      string    `json:"base_commit"`
	Path      string    `json:"path"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Last report from the runner: never secrets, always evidence.
	// Populated on completion so a FAILED workspace explains itself.
	LastExit   int    `json:"last_exit,omitempty"`
	LastStdout string `json:"last_stdout,omitempty"`
	LastStderr string `json:"last_stderr,omitempty"`
	LastTest   int    `json:"last_test_exit,omitempty"`
}

// Manager creates worktrees under Root and tracks them.
type Manager struct {
	mu   sync.Mutex
	Root string
	ws   map[string]*Workspace
}

// NewManager roots worktrees at root (runner-owned, disposable).
// The root is absolutized: worktree paths are stored absolute so they
// resolve identically for git (which anchors relative paths at the
// repo) and for the server (which anchors at its own cwd). Relative
// paths silently split those two and break diffs, builds, and merges.
func NewManager(root string) *Manager {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return &Manager{Root: root, ws: map[string]*Workspace{}}
}

// Create pins base HEAD and adds a detached worktree. Emits no events
// itself — the API layer publishes workspace.created/ready.
func (m *Manager) Create(ctx context.Context, projectID, taskID, repo string) (*Workspace, error) {
	base, err := git.Head(ctx, repo, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read base: %w", err)
	}
	return m.CreateAt(ctx, projectID, taskID, repo, base)
}

func (m *Manager) CreateAt(ctx context.Context, projectID, taskID, repo, base string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := &Workspace{ID: uuid.NewString(), ProjectID: projectID, TaskID: taskID,
		RepoPath: repo, Base: base, Path: filepath.Join(m.Root, uuid.NewString()),
		Status: Creating, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := git.AddWorktree(ctx, repo, w.Path, base); err != nil {
		return nil, fmt.Errorf("add worktree: %w", err)
	}
	w.Status = Ready
	w.UpdatedAt = time.Now().UTC()
	m.ws[w.ID] = w
	copy := *w
	return &copy, nil
}

// SetStatus moves lifecycle forward (no skipping from DESTROYED).
func (m *Manager) SetStatus(id string, s Status) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.ws[id]
	if !ok {
		return nil, fmt.Errorf("unknown workspace %s", id)
	}
	if w.Status == Destroyed {
		return nil, fmt.Errorf("workspace destroyed")
	}
	switch s {
	case Creating, Ready, Running, Waiting, Completed, Failed, Conflicted, Destroyed:
	default:
		return nil, fmt.Errorf("unknown workspace status %s", s)
	}
	if (w.Status == Completed || w.Status == Failed || w.Status == Conflicted) && s != w.Status && s != Destroyed {
		return nil, fmt.Errorf("terminal workspace cannot resume")
	}
	w.Status = s
	w.UpdatedAt = time.Now().UTC()
	copy := *w
	return &copy, nil
}

// SetReport records the runner's outcome on the workspace: status plus
// the evidence (exit codes, capped transcript). Oldest fix for silent
// failures — a FAILED workspace must explain itself on read.
func (m *Manager) SetReport(id string, s Status, exit int, stdout, stderr string, testExit int) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.ws[id]
	if !ok {
		return nil, fmt.Errorf("unknown workspace %s", id)
	}
	if w.Status == Destroyed {
		return nil, fmt.Errorf("workspace destroyed")
	}
	switch s {
	case Creating, Ready, Running, Waiting, Completed, Failed, Conflicted, Destroyed:
	default:
		return nil, fmt.Errorf("unknown workspace status %s", s)
	}
	w.Status = s
	w.UpdatedAt = time.Now().UTC()
	w.LastExit = exit
	w.LastStdout = capStr(stdout, 20000)
	w.LastStderr = capStr(stderr, 8000)
	w.LastTest = testExit
	copy := *w
	return &copy, nil
}

func capStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}
func (m *Manager) ChangedFiles(ctx context.Context, id string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.ws[id]
	if !ok {
		return nil, fmt.Errorf("unknown workspace %s", id)
	}
	return git.ChangedBase(ctx, w.Path, w.Base)
}

// Diff returns the full unified diff vs base (for changesets/review).
func (m *Manager) Diff(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.ws[id]
	if !ok {
		return "", fmt.Errorf("unknown workspace %s", id)
	}
	return git.DiffBase(ctx, w.Path, w.Base)
}

// Checkpoint commits current work (agent/test checkpoints are commits,
// never rewrites of shared history).
func (m *Manager) Checkpoint(ctx context.Context, id, message string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.ws[id]
	if !ok {
		return "", fmt.Errorf("unknown workspace %s", id)
	}
	return git.Checkpoint(ctx, w.Path, message)
}

// Destroy removes the worktree. Failed workspaces must be explicitly
// destroyed so nothing with debug value disappears silently.
func (m *Manager) Destroy(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.ws[id]
	if !ok {
		return fmt.Errorf("unknown workspace %s", id)
	}
	if err := git.RemoveWorktree(ctx, w.RepoPath, w.Path); err != nil {
		return err
	}
	w.Status = Destroyed
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Attach re-tracks a surviving worktree (e.g. after a restart) without
// touching the repository.
func (m *Manager) Attach(w *Workspace) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *w
	m.ws[w.ID] = &copy
}

// Get returns a tracked workspace.
func (m *Manager) Get(id string) (*Workspace, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.ws[id]
	if !ok {
		return nil, false
	}
	copy := *w
	return &copy, true
}

// Active lists non-terminal workspaces for a project.
func (m *Manager) Active(projectID string) []*Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Workspace
	for _, w := range m.ws {
		if w.ProjectID != projectID {
			continue
		}
		switch w.Status {
		case Creating, Ready, Running, Waiting, Conflicted:
			copy := *w
			out = append(out, &copy)
		}
	}
	return out
}
