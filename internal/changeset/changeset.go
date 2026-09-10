// Package changeset turns worktree state into reviewable proposals.
// Agents never merge: they produce changesets (base, files, diff, tests).
// Integration revalidates each changeset against the CURRENT canonical
// head; losers become NEEDS_REBASE or CONFLICTED — never force-merged.
package changeset

import (
	"context"
	"time"

	"ballast/internal/git"
	"github.com/google/uuid"
)

// Status of a changeset.
type Status string

const (
	Draft       Status = "DRAFT"
	InReview    Status = "IN_REVIEW"
	Approved    Status = "APPROVED"
	Rejected    Status = "REJECTED"
	Merged      Status = "MERGED"
	NeedsRebase Status = "NEEDS_REBASE"
	Conflicted  Status = "CONFLICTED"
)

// Changeset is the reviewable unit.
type Changeset struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	TaskID    string    `json:"task_id"`
	AgentID   string    `json:"agent_id,omitempty"`
	Base      string    `json:"base_commit"`
	Files     []string  `json:"files"`
	Diff      string    `json:"diff"`
	TestRef   string    `json:"test_ref,omitempty"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Build snapshots a worktree into a changeset (IN_REVIEW).
func Build(ctx context.Context, projectID, taskID, agentID, workPath, base string) (*Changeset, error) {
	files, err := git.ChangedBase(ctx, workPath, base)
	if err != nil {
		return nil, err
	}
	diff, err := git.DiffBase(ctx, workPath, base)
	if err != nil {
		return nil, err
	}
	return &Changeset{ID: uuid.NewString(), ProjectID: projectID, TaskID: taskID,
		AgentID: agentID, Base: base, Files: files, Diff: diff,
		Status: InReview, CreatedAt: time.Now().UTC()}, nil
}
