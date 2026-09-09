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
