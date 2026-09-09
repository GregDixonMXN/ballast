// Package services implements the api service interfaces over a Repo
// (record persistence: Postgres or memory) plus the workspace Manager
// (worktree execution truth). One implementation, two backends —
// services never know which Repo they run against.
package services

import (
	"context"
	"fmt"

	"ballast/internal/changeset"
	"ballast/internal/git"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

// Repo persists domain records. Memory and Postgres implementations live
// in internal/store. Upsert semantics: Save creates or overwrites by ID.
type Repo interface {
	SaveProject(ctx context.Context, p project.Project) error
	GetProject(ctx context.Context, id string) (project.Project, error)
	SetCanonicalHead(ctx context.Context, id, sha string) error

	SaveTask(ctx context.Context, t task.Task) error
	GetTask(ctx context.Context, id string) (task.Task, error)
	ListTasks(ctx context.Context, projectID string) ([]task.Task, error)

	SaveWorkspace(ctx context.Context, w workspace.Workspace) error
	GetWorkspace(ctx context.Context, id string) (workspace.Workspace, error)
	ListWorkspaces(ctx context.Context, projectID string) ([]workspace.Workspace, error)

	SaveChangeset(ctx context.Context, c changeset.Changeset) error
	GetChangeset(ctx context.Context, id string) (changeset.Changeset, error)
}

// Projects owns project records; canonical head moves only via integration.
type Projects struct {
	Repo Repo
}

func (s *Projects) Create(name, repo, branch string) (any, error) {
	ctx := context.Background()
	if name == "" || repo == "" {
		return nil, fmt.Errorf("name and repo required")
	}
	if branch == "" {
		branch = "main"
	}
	head, err := git.Head(ctx, repo, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("repository not readable: %w", err)
	}
	p := project.NewProject("", name, repo, branch)
	p.CanonicalSHA = head
	if err := s.Repo.SaveProject(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Projects) Get(id string) (any, error) {
	return s.Repo.GetProject(context.Background(), id)
}

func (s *Projects) Head(id string) (string, error) {
	ctx := context.Background()
	p, err := s.Repo.GetProject(ctx, id)
	if err != nil {
		return "", err
	}
	return git.Head(ctx, p.RepoPath, p.Branch)
}

// Tasks owns the board. Transitions are guarded by task rules.
type Tasks struct {
	Repo Repo
}

func (s *Tasks) Create(projectID, title, desc string, scopes []string) (any, error) {
	ctx := context.Background()
	if title == "" {
		return nil, fmt.Errorf("title required")
	}
	if _, err := s.Repo.GetProject(ctx, projectID); err != nil {
		return nil, fmt.Errorf("unknown project: %w", err)
	}
	t := task.New(projectID, title, desc, scopes)
	if err := s.Repo.SaveTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Tasks) List(projectID string) ([]any, error) {
	ts, err := s.Repo.ListTasks(context.Background(), projectID)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(ts))
	for i, t := range ts {
		out[i] = t
	}
	return out, nil
}

func (s *Tasks) Transition(id, to string) (any, error) {
	ctx := context.Background()
	t, err := s.Repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if !t.Transition(task.Status(to)) {
		return nil, fmt.Errorf("illegal transition %s → %s", t.Status, to)
	}
	if err := s.Repo.SaveTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Workspaces binds tasks to isolated worktrees. The Manager owns the
// directories; the Repo owns the records. Reattach re-links records to
// the Manager after a restart (worktrees survive, memory doesn't).
type Workspaces struct {
	Repo Repo
	Mgr  *workspace.Manager
}

// Reattach re-tracks surviving worktree paths after a restart.
func (s *Workspaces) Reattach(ctx context.Context, projectID string) (int, error) {
	list, err := s.Repo.ListWorkspaces(ctx, projectID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, w := range list {
		switch w.Status {
		case workspace.Creating, workspace.Ready, workspace.Running,
			workspace.Waiting, workspace.Conflicted:
			w := w
			s.Mgr.Attach(&w)
			n++
		}
	}
	return n, nil
}

func (s *Workspaces) Create(projectID, taskID, repo string) (any, error) {
	ctx := context.Background()
	if _, err := s.Repo.GetProject(ctx, projectID); err != nil {
		return nil, fmt.Errorf("unknown project: %w", err)
	}
	if _, err := s.Repo.GetTask(ctx, taskID); err != nil {
		return nil, fmt.Errorf("unknown task: %w", err)
	}
	if repo == "" {
		p, err := s.Repo.GetProject(ctx, projectID)
		if err != nil {
			return nil, err
		}
		repo = p.RepoPath
	}
	w, err := s.Mgr.Create(ctx, projectID, taskID, repo)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.SaveWorkspace(ctx, *w); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Workspaces) List(projectID string) ([]any, error) {
	list, err := s.Repo.ListWorkspaces(context.Background(), projectID)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(list))
	for i, w := range list {
		out[i] = w
	}
	return out, nil
}

func (s *Workspaces) ChangedFiles(id string) ([]string, error) {
	return s.Mgr.ChangedFiles(context.Background(), id)
}

func (s *Workspaces) Diff(id string) (string, error) {
	return s.Mgr.Diff(context.Background(), id)
}

// Changesets turns worktree state into reviewable proposals.
// Approval here marks intent; controlled integration (merge) is the
// next milestone and revalidates against the live canonical head.
type Changesets struct {
	Repo Repo
}

func (s *Changesets) Build(projectID, taskID, agentID, wsID string) (any, error) {
	ctx := context.Background()
	w, err := s.Repo.GetWorkspace(ctx, wsID)
	if err != nil {
		return nil, err
	}
	cs, err := changeset.Build(ctx, projectID, taskID, agentID, w.Path, w.Base)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.SaveChangeset(ctx, *cs); err != nil {
		return nil, err
	}
	return cs, nil
}

func (s *Changesets) Get(id string) (any, error) {
	return s.Repo.GetChangeset(context.Background(), id)
}

func (s *Changesets) Decide(id, decision, _ string) (any, error) {
	ctx := context.Background()
	cs, err := s.Repo.GetChangeset(ctx, id)
	if err != nil {
		return nil, err
	}
	switch decision {
	case "approve":
		if cs.Status != changeset.InReview && cs.Status != changeset.NeedsRebase {
			return nil, fmt.Errorf("cannot approve from %s", cs.Status)
		}
		cs.Status = changeset.Approved
	case "reject":
		cs.Status = changeset.Rejected
	default:
		return nil, fmt.Errorf("decision must be approve|reject")
	}
	if err := s.Repo.SaveChangeset(ctx, cs); err != nil {
		return nil, err
	}
	return cs, nil
}
