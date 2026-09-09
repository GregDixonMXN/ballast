// Package workspace manages isolated Git worktrees bound to tasks.
// No two workers ever share a mutable directory: one workspace = one
// worktree path = one task assignment. Failed workspaces are preserved
// for forensics; only explicit destroy removes them.
package workspace

import (
	"context"
	"fmt"
	"path/filepath"
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
}

// Manager creates worktrees under Root and tracks them.
type Manager struct {
	Root string
	ws   map[string]*Workspace
}

// NewManager roots worktrees at root (runner-owned, disposable).
func NewManager(root string) *Manager { return &Manager{Root: root, ws: map[string]*Workspace{}} }

// Create pins base HEAD and adds a detached worktree. Emits no events
// itself — the API layer publishes workspace.created/ready.
func (m *Manager) Create(ctx context.Context, projectID, taskID, repo string) (*Workspace, error) {
	base, err := git.Head(ctx, repo, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read base: %w", err)
	}
	w := &Workspace{ID: uuid.NewString(), ProjectID: projectID, TaskID: taskID,
		RepoPath: repo, Base: base, Path: filepath.Join(m.Root, uuid.NewString()),
		Status: Creating, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := git.AddWorktree(ctx, repo, w.Path, base); err != nil {
		return nil, fmt.Errorf("add worktree: %w", err)
	}
	w.Status = Ready
	w.UpdatedAt = time.Now().UTC()
	m.ws[w.ID] = w
	return w, nil
}

// SetStatus moves lifecycle forward (no skipping from DESTROYED).
func (m *Manager) SetStatus(id string, s Status) (*Workspace, error) {
	w, ok := m.ws[id]
	if !ok {
		return nil, fmt.Errorf("unknown workspace %s", id)
	}
	if w.Status == Destroyed {
		return nil, fmt.Errorf("workspace destroyed")
	}
	w.Status = s
	w.UpdatedAt = time.Now().UTC()
	return w, nil
}

// ChangedFiles lists worktree modifications vs its base commit.
func (m *Manager) ChangedFiles(ctx context.Context, id string) ([]string, error) {
	w, ok := m.ws[id]
	if !ok {
		return nil, fmt.Errorf("unknown workspace %s", id)
	}
	return git.StatusPorcelain(ctx, w.Path)
}

// Diff returns the full unified diff vs base (for changesets/review).
func (m *Manager) Diff(ctx context.Context, id string) (string, error) {
	w, ok := m.ws[id]
	if !ok {
		return "", fmt.Errorf("unknown workspace %s", id)
	}
	return git.DiffBase(ctx, w.Path, w.Base)
}

// Checkpoint commits current work (agent/test checkpoints are commits,
// never rewrites of shared history).
func (m *Manager) Checkpoint(ctx context.Context, id, message string) (string, error) {
	w, ok := m.ws[id]
	if !ok {
		return "", fmt.Errorf("unknown workspace %s", id)
	}
	return git.Checkpoint(ctx, w.Path, message)
}

// Destroy removes the worktree. Failed workspaces must be explicitly
// destroyed so nothing with debug value disappears silently.
func (m *Manager) Destroy(ctx context.Context, id string) error {
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
func (m *Manager) Attach(w *Workspace) { m.ws[w.ID] = w }

// Get returns a tracked workspace.
func (m *Manager) Get(id string) (*Workspace, bool) { w, ok := m.ws[id]; return w, ok }

// Active lists non-terminal workspaces for a project.
func (m *Manager) Active(projectID string) []*Workspace {
	var out []*Workspace
	for _, w := range m.ws {
		if w.ProjectID != projectID {
			continue
		}
		switch w.Status {
		case Creating, Ready, Running, Waiting, Conflicted:
			out = append(out, w)
		}
	}
	return out
}
