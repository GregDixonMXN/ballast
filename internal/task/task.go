// Package task owns task lifecycle and assignment. Tasks are the unit of
// human intent; workspaces are the unit of execution. One task may fan out
// to several workspaces (agents) over time, but only one active workspace
// per task in the MVP.
package task

import (
	"time"

	"github.com/google/uuid"
)

// Status of a task on the board.
type Status string

const (
	Todo    Status = "TODO"
	Running Status = "RUNNING"
	Blocked Status = "BLOCKED"
	Review  Status = "REVIEW"
	Done    Status = "DONE"
)

// Task is created by humans, executed by agents in workspaces.
type Task struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Scopes      []string  `json:"scopes,omitempty"`
	DependsOn   []string  `json:"depends_on,omitempty"`
	TestCommand string    `json:"test_command,omitempty"`
	Command     string    `json:"command,omitempty"` // dumb-runner TASK_CMD: shell run in the worktree (e.g. `sh -c '…'` body, `claude -p "…"`)
	Status      Status    `json:"status"`
	AssigneeID  string    `json:"assignee_id,omitempty"` // agent instance or user
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// New validates and builds a TODO task.
func New(projectID, title, desc string, scopes []string) Task {
	now := time.Now().UTC()
	return Task{ID: uuid.NewString(), ProjectID: projectID, Title: title,
		Description: desc, Scopes: scopes, Status: Todo, CreatedAt: now, UpdatedAt: now}
}

// Ready reports whether every dependency is DONE. A task with no
// dependencies is always ready.
func Ready(t Task, byID map[string]Task) bool {
	for _, dep := range t.DependsOn {
		d, ok := byID[dep]
		if !ok || d.Status != Done {
			return false
		}
	}
	return true
}

// Transition enforces the legal board moves.
func (t *Task) Transition(to Status) bool {
	ok := map[Status][]Status{
		Todo:    {Running, Blocked},
		Running: {Blocked, Review, Todo},
		Blocked: {Todo, Running},
		Review:  {Done, Running, Blocked},
		Done:    {},
	}[t.Status]
	for _, s := range ok {
		if s == to {
			t.Status = to
			t.UpdatedAt = time.Now().UTC()
			return true
		}
	}
	return false
}
