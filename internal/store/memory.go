package store

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"ballast/internal/changeset"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

// MemoryRepo is the zero-dependency Repo: process-local maps for dev,
// tests, and running without DATABASE_URL. Same semantics as PGRepo.
type MemoryRepo struct {
	mu sync.RWMutex
	pr map[string]project.Project
	ta map[string]task.Task
	ws map[string]workspace.Workspace
	cs map[string]changeset.Changeset
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{
		pr: map[string]project.Project{},
		ta: map[string]task.Task{},
		ws: map[string]workspace.Workspace{},
		cs: map[string]changeset.Changeset{},
	}
}

func (m *MemoryRepo) SaveProject(_ context.Context, p project.Project) error {
	m.mu.Lock()
	m.pr[p.ID] = p
	m.mu.Unlock()
	return nil
}

func (m *MemoryRepo) GetProject(_ context.Context, id string) (project.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pr[id]
	if !ok {
		return project.Project{}, fmt.Errorf("unknown project %s", id)
	}
	return p, nil
}

func (m *MemoryRepo) SetCanonicalHead(_ context.Context, id, sha string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pr[id]
	if !ok {
		return fmt.Errorf("unknown project %s", id)
	}
	p.CanonicalSHA = sha
	m.pr[id] = p
	return nil
}

func (m *MemoryRepo) SaveTask(_ context.Context, t task.Task) error {
	m.mu.Lock()
	m.ta[t.ID] = t
	m.mu.Unlock()
	return nil
}

func (m *MemoryRepo) GetTask(_ context.Context, id string) (task.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.ta[id]
	if !ok {
		return task.Task{}, fmt.Errorf("unknown task %s", id)
	}
	return t, nil
}

func (m *MemoryRepo) ListTasks(_ context.Context, projectID string) ([]task.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []task.Task
	for _, t := range m.ta {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryRepo) SaveWorkspace(_ context.Context, w workspace.Workspace) error {
	m.mu.Lock()
	m.ws[w.ID] = w
	m.mu.Unlock()
	return nil
}

func (m *MemoryRepo) GetWorkspace(_ context.Context, id string) (workspace.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.ws[id]
	if !ok {
		return workspace.Workspace{}, fmt.Errorf("unknown workspace %s", id)
	}
	return w, nil
}

func (m *MemoryRepo) ListWorkspaces(_ context.Context, projectID string) ([]workspace.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []workspace.Workspace
	for _, w := range m.ws {
		if w.ProjectID == projectID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryRepo) SaveChangeset(_ context.Context, c changeset.Changeset) error {
	m.mu.Lock()
	m.cs[c.ID] = c
	m.mu.Unlock()
	return nil
}

func (m *MemoryRepo) GetChangeset(_ context.Context, id string) (changeset.Changeset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.cs[id]
	if !ok {
		return changeset.Changeset{}, fmt.Errorf("unknown changeset %s", id)
	}
	return c, nil
}
