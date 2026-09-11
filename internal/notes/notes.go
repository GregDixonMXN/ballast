// Package notes is the project's blackboard: short agent-to-agent
// messages (discoveries, warnings, conventions) shared across runs.
// In-memory, capped per project, newest first. Durability is a
// non-goal: the task record, not the board, is the source of truth.
package notes

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Note is one board entry. Author is a workspace or task id, or
// "operator" for humans. Never secrets.
type Note struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// MaxPerProject bounds memory and prompt-injection size.
const MaxPerProject = 50

// MaxText caps one note; boards are signals, not documents.
const MaxText = 2000

// Board holds notes per project.
type Board struct {
	mu sync.Mutex
	by map[string][]Note
}

func New() *Board { return &Board{by: map[string][]Note{}} }

// Post appends a note, trimming text and evicting oldest beyond cap.
func (b *Board) Post(projectID, author, text string) Note {
	if len(text) > MaxText {
		text = text[:MaxText] + "...[trimmed]"
	}
	n := Note{ID: uuid.NewString(), ProjectID: projectID, Author: author, Text: text, CreatedAt: time.Now().UTC()}
	b.mu.Lock()
	defer b.mu.Unlock()
	l := append(b.by[projectID], n)
	if len(l) > MaxPerProject {
		l = l[len(l)-MaxPerProject:]
	}
	b.by[projectID] = l
	return n
}

// List returns newest first.
func (b *Board) List(projectID string) []Note {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := b.by[projectID]
	out := make([]Note, len(l))
	for i, n := range l {
		out[len(l)-1-i] = n
	}
	return out
}
