// Package services implements the api service interfaces over a Repo
// (record persistence: Postgres or memory) plus the workspace Manager
// (worktree execution truth). One implementation, two backends —
// services never know which Repo they run against.
package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"ballast/internal/changeset"
	"ballast/internal/git"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

// Repo persists domain records. Memory and Postgres implementations live
// in internal/store. Upsert semantics: Save creates or overwrites by ID.
type Repo interface {
	ListProjects(ctx context.Context) ([]project.Project, error)
	SaveProject(ctx context.Context, p project.Project) error
	GetProject(ctx context.Context, id string) (project.Project, error)
	SetCanonicalHead(ctx context.Context, id, sha string) error

	SaveTask(ctx context.Context, t task.Task) error
	GetTask(ctx context.Context, id string) (task.Task, error)
	ListTasks(ctx context.Context, projectID string) ([]task.Task, error)
	DeleteTask(ctx context.Context, id string) error

	SaveWorkspace(ctx context.Context, w workspace.Workspace) error
	GetWorkspace(ctx context.Context, id string) (workspace.Workspace, error)
	ListWorkspaces(ctx context.Context, projectID string) ([]workspace.Workspace, error)
	DeleteWorkspace(ctx context.Context, id string) error

	SaveChangeset(ctx context.Context, c changeset.Changeset) error
	GetChangeset(ctx context.Context, id string) (changeset.Changeset, error)
	ListChangesets(ctx context.Context, projectID string) ([]changeset.Changeset, error)
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
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		return nil, err
	}
	if err = git.ValidateBranch(ctx, repo, branch); err != nil {
		return nil, err
	}
	head, err := git.Head(ctx, repo, "refs/heads/"+branch)
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
	mu   sync.Mutex
	Repo Repo
	Mgr  *workspace.Manager // optional: cascade worktree removal on delete
}

// ErrTaskRunning refuses mutation while a runner holds the task.
var ErrTaskRunning = errors.New("task is running")

func (s *Tasks) Create(projectID, title, desc string, scopes []string) (any, error) {
	return s.CreateWithDeps(projectID, title, desc, scopes, nil)
}

// CreateWithDeps creates a TODO task with dependency edges. Unknown dep
// IDs are rejected so the graph stays valid.
func (s *Tasks) CreateWithDeps(projectID, title, desc string, scopes, depends []string) (any, error) {
	ctx := context.Background()
	if title == "" {
		return nil, fmt.Errorf("title required")
	}
	if _, err := s.Repo.GetProject(ctx, projectID); err != nil {
		return nil, fmt.Errorf("unknown project: %w", err)
	}
	t := task.New(projectID, title, desc, scopes)
	t.DependsOn = depends
	if err := s.Repo.SaveTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Ready returns TODO tasks whose dependencies are all DONE.
func (s *Tasks) Ready(projectID string) ([]any, error) {
	ts, err := s.Repo.ListTasks(context.Background(), projectID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]task.Task, len(ts))
	for _, t := range ts {
		byID[t.ID] = t
	}
	var out []any
	for _, t := range ts {
		if t.Status == task.Todo && task.Ready(t, byID) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *Tasks) Get(id string) (any, error) {
	return s.Repo.GetTask(context.Background(), id)
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
	s.mu.Lock()
	defer s.mu.Unlock()
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

// Update rewrites title/description/scopes. Refused while RUNNING —
// the runner holds a prompt snapshot taken at assignment.
func (s *Tasks) Update(id, title, desc string, scopes []string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	t, err := s.Repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Status == task.Running {
		return nil, ErrTaskRunning
	}
	if title != "" {
		t.Title = title
	}
	t.Description = desc
	if scopes != nil {
		t.Scopes = scopes
	}
	t.UpdatedAt = time.Now().UTC()
	if err := s.Repo.SaveTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Delete removes the task and its workspaces (records + worktree
// dirs). Refused while RUNNING — requeue first. Changesets stay:
// review history outlives the task that produced it.
func (s *Tasks) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	t, err := s.Repo.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if t.Status == task.Running {
		return ErrTaskRunning
	}
	ws, err := s.Repo.ListWorkspaces(ctx, t.ProjectID)
	if err != nil {
		return err
	}
	for _, w := range ws {
		if w.TaskID != id {
			continue
		}
		if s.Mgr != nil {
			_ = s.Mgr.Destroy(ctx, w.ID)
		}
		if err := s.Repo.DeleteWorkspace(ctx, w.ID); err != nil {
			return err
		}
	}
	return s.Repo.DeleteTask(ctx, id)
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
		if w.Status != workspace.Destroyed {
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
	t, err := s.Repo.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("unknown task: %w", err)
	}
	if t.ProjectID != projectID {
		return nil, fmt.Errorf("task does not belong to project")
	}
	p, err := s.Repo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if repo != "" && repo != p.RepoPath {
		return nil, fmt.Errorf("repository must match project")
	}
	base, err := git.Head(ctx, p.RepoPath, "refs/heads/"+p.Branch)
	if err != nil {
		return nil, err
	}
	w, err := s.Mgr.CreateAt(ctx, projectID, taskID, p.RepoPath, base)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.SaveWorkspace(ctx, *w); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Workspaces) Get(id string) (any, error) {
	w, err := s.Repo.GetWorkspace(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Workspaces) SetStatus(id, status string) (any, error) {
	ctx := context.Background()
	updated, err := s.Mgr.SetStatus(id, workspace.Status(status))
	if err != nil {
		return nil, err
	}
	if err := s.Repo.SaveWorkspace(ctx, *updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// SetReport persists a completion report (status + evidence) in one step.
func (s *Workspaces) SetReport(id, status string, exit int, stdout, stderr string, testExit int) (any, error) {
	ctx := context.Background()
	updated, err := s.Mgr.SetReport(id, workspace.Status(status), exit, stdout, stderr, testExit)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.SaveWorkspace(ctx, *updated); err != nil {
		return nil, err
	}
	return updated, nil
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
	if projectID != "" && projectID != w.ProjectID || taskID != "" && taskID != w.TaskID {
		return nil, fmt.Errorf("workspace ownership mismatch")
	}
	cs, err := changeset.Build(ctx, w.ProjectID, w.TaskID, agentID, w.Path, w.Base)
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
		if cs.Status != changeset.InReview {
			return nil, fmt.Errorf("cannot approve from %s", cs.Status)
		}
		cs.Status = changeset.Approved
	case "reject":
		if cs.Status != changeset.InReview && cs.Status != changeset.Approved {
			return nil, fmt.Errorf("cannot reject from %s", cs.Status)
		}
		cs.Status = changeset.Rejected
	default:
		return nil, fmt.Errorf("decision must be approve|reject")
	}
	if err := s.Repo.SaveChangeset(ctx, cs); err != nil {
		return nil, err
	}
	return cs, nil
}

func (s *Projects) List() ([]project.Project, error) {
	return s.Repo.ListProjects(context.Background())
}

// Recover preserves files and blocks interrupted execution. Commands are never
// automatically replayed after a control-plane restart.
func Recover(ctx context.Context, repo Repo, mgr *workspace.Manager) error {
	ps, err := repo.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range ps {
		ws, err := repo.ListWorkspaces(ctx, p.ID)
		if err != nil {
			return err
		}
		for _, w := range ws {
			if w.Status == workspace.Destroyed {
				continue
			}
			if w.Status == workspace.Creating || w.Status == workspace.Ready || w.Status == workspace.Running || w.Status == workspace.Waiting {
				w.Status = workspace.Failed
				if err := repo.SaveWorkspace(ctx, w); err != nil {
					return err
				}
			}
			mgr.Attach(&w)
		}
		ts, err := repo.ListTasks(ctx, p.ID)
		if err != nil {
			return err
		}
		for _, t := range ts {
			if t.Status == task.Running {
				t.Transition(task.Blocked)
				if err := repo.SaveTask(ctx, t); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
