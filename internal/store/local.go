package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	"ballast/internal/changeset"
	"ballast/internal/events"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

// LocalRepo atomically publishes immutable snapshots under a lifetime file
// lock. A failed write before rename never changes readable state. If directory
// sync fails after rename, the outcome is uncertain and the store fails closed
// until reopened; no later write can silently persist an unacknowledged change.
type LocalRepo struct {
	mu      sync.RWMutex
	path    string
	lock    *os.File
	state   snapshot
	fault   error
	syncDir func(string) error
}
type snapshot struct {
	Version    int                            `json:"version"`
	Projects   map[string]project.Project     `json:"projects"`
	Tasks      map[string]task.Task           `json:"tasks"`
	Workspaces map[string]workspace.Workspace `json:"workspaces"`
	Changesets map[string]changeset.Changeset `json:"changesets"`
	Events     []events.Event                 `json:"events"`
}

func emptySnapshot() snapshot {
	return snapshot{Version: 1, Projects: map[string]project.Project{}, Tasks: map[string]task.Task{}, Workspaces: map[string]workspace.Workspace{}, Changesets: map[string]changeset.Changeset{}, Events: []events.Event{}}
}
func privateFile(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		err = fmt.Errorf("%s must be a private regular file (0600)", path)
	}
	if err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Geteuid()) {
			err = fmt.Errorf("%s must be owned by the current user", path)
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Persist newly created directory entries as well as the eventual snapshot;
// syncing a new directory alone does not persist its name in its parent.
func makeDurableDirectory(path string) error {
	missing := []string{}
	for dir := path; ; dir = filepath.Dir(dir) {
		_, err := os.Stat(dir)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	for _, dir := range missing {
		if err := syncDirectory(filepath.Dir(dir)); err != nil {
			return err
		}
	}
	return nil
}
func OpenLocal(path string) (*LocalRepo, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = makeDurableDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := privateFile(path+".lock", syscall.O_CREAT|syscall.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("local store already in use: %w", err)
	}
	l := &LocalRepo{path: path, lock: f, state: emptySnapshot(), syncDir: syncDirectory}
	data, err := privateFile(path, syscall.O_RDONLY)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		l.Close()
		return nil, err
	}
	b, err := io.ReadAll(data)
	closeErr := data.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		l.state = snapshot{}
		err = json.Unmarshal(b, &l.state)
	}
	if err == nil {
		err = validateSnapshot(l.state)
	}
	if err != nil {
		l.Close()
		return nil, fmt.Errorf("invalid local store: %w", err)
	}
	return l, nil
}
func validateSnapshot(s snapshot) error {
	if s.Version != 1 || s.Projects == nil || s.Tasks == nil || s.Workspaces == nil || s.Changesets == nil {
		return errors.New("unsupported or incomplete snapshot")
	}
	for id, v := range s.Projects {
		if id == "" || v.ID != id {
			return errors.New("invalid project identity")
		}
	}
	for id, v := range s.Tasks {
		if id == "" || v.ID != id {
			return errors.New("invalid task identity")
		}
		if _, ok := s.Projects[v.ProjectID]; !ok {
			return errors.New("task references unknown project")
		}
	}
	for id, v := range s.Workspaces {
		t, ok := s.Tasks[v.TaskID]
		if id == "" || v.ID != id || !ok || t.ProjectID != v.ProjectID {
			return errors.New("invalid workspace ownership")
		}
	}
	for id, v := range s.Changesets {
		t, ok := s.Tasks[v.TaskID]
		if id == "" || v.ID != id || !ok || t.ProjectID != v.ProjectID {
			return errors.New("invalid changeset ownership")
		}
	}
	return nil
}
func (l *LocalRepo) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.lock == nil {
		return errors.New("local store is closed")
	}
	if l.fault != nil {
		return l.fault
	}
	actual, err := os.Lstat(l.path + ".lock")
	if err != nil {
		return fmt.Errorf("local store lock unavailable: %w", err)
	}
	held, err := l.lock.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(actual, held) || !actual.Mode().IsRegular() || actual.Mode().Perm()&0077 != 0 {
		return errors.New("local store lock was replaced or made public; reopen storage")
	}
	return nil
}
func (l *LocalRepo) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lock == nil {
		return nil
	}
	err := l.lock.Close()
	l.lock = nil
	return err
}
func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (l *LocalRepo) persist(b []byte) (published bool, err error) {
	f, err := os.CreateTemp(filepath.Dir(l.path), ".ballast-state-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return false, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return false, err
	}
	if err = f.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(f.Name(), l.path); err != nil {
		return false, err
	}
	return true, l.syncDir(filepath.Dir(l.path))
}
func (l *LocalRepo) update(ctx context.Context, change func(*snapshot) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.check(ctx); err != nil {
		return err
	}
	b, err := json.Marshal(l.state)
	if err != nil {
		return err
	}
	var candidate snapshot
	if err = json.Unmarshal(b, &candidate); err != nil {
		return err
	}
	if err = change(&candidate); err != nil {
		return err
	}
	if err = validateSnapshot(candidate); err != nil {
		return err
	}
	b, err = json.Marshal(candidate)
	if err != nil {
		return err
	}
	// Detach caller-owned slices and metadata before publication.
	var committed snapshot
	if err = json.Unmarshal(b, &committed); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	published, err := l.persist(b)
	if err != nil {
		if published {
			l.state = committed
			l.fault = fmt.Errorf("snapshot published but durability sync failed; close and reopen local storage: %w", err)
			return l.fault
		}
		return err
	}
	l.state = committed
	return nil
}
func (l *LocalRepo) SaveProject(ctx context.Context, p project.Project) error {
	return l.update(ctx, func(s *snapshot) error { s.Projects[p.ID] = p; return nil })
}
func (l *LocalRepo) SetCanonicalHead(ctx context.Context, id, sha string) error {
	return l.update(ctx, func(s *snapshot) error {
		p, ok := s.Projects[id]
		if !ok {
			return fmt.Errorf("unknown project %s", id)
		}
		p.CanonicalSHA = sha
		s.Projects[id] = p
		return nil
	})
}
func (l *LocalRepo) SaveTask(ctx context.Context, t task.Task) error {
	return l.update(ctx, func(s *snapshot) error { s.Tasks[t.ID] = t; return nil })
}
func (l *LocalRepo) SaveWorkspace(ctx context.Context, w workspace.Workspace) error {
	return l.update(ctx, func(s *snapshot) error { s.Workspaces[w.ID] = w; return nil })
}
func (l *LocalRepo) SaveChangeset(ctx context.Context, c changeset.Changeset) error {
	return l.update(ctx, func(s *snapshot) error { s.Changesets[c.ID] = c; return nil })
}
func (l *LocalRepo) Append(ctx context.Context, e events.Event) error {
	return l.update(ctx, func(s *snapshot) error { s.Events = append(s.Events, e); return nil })
}
func (l *LocalRepo) GetProject(ctx context.Context, id string) (project.Project, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return project.Project{}, err
	}
	v, ok := l.state.Projects[id]
	if !ok {
		return v, fmt.Errorf("unknown project %s", id)
	}
	return v, nil
}
func (l *LocalRepo) ListProjects(ctx context.Context) ([]project.Project, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return nil, err
	}
	out := []project.Project{}
	for _, v := range l.state.Projects {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func cloneTask(t task.Task) task.Task { t.Scopes = append([]string(nil), t.Scopes...); return t }
func cloneChangeset(c changeset.Changeset) changeset.Changeset {
	c.Files = append([]string(nil), c.Files...)
	return c
}
func (l *LocalRepo) GetTask(ctx context.Context, id string) (task.Task, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return task.Task{}, err
	}
	v, ok := l.state.Tasks[id]
	if !ok {
		return v, fmt.Errorf("unknown task %s", id)
	}
	return cloneTask(v), nil
}
func (l *LocalRepo) ListTasks(ctx context.Context, pid string) ([]task.Task, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return nil, err
	}
	out := []task.Task{}
	for _, v := range l.state.Tasks {
		if v.ProjectID == pid {
			out = append(out, cloneTask(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (l *LocalRepo) GetWorkspace(ctx context.Context, id string) (workspace.Workspace, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return workspace.Workspace{}, err
	}
	v, ok := l.state.Workspaces[id]
	if !ok {
		return v, fmt.Errorf("unknown workspace %s", id)
	}
	return v, nil
}
func (l *LocalRepo) ListWorkspaces(ctx context.Context, pid string) ([]workspace.Workspace, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return nil, err
	}
	out := []workspace.Workspace{}
	for _, v := range l.state.Workspaces {
		if v.ProjectID == pid {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (l *LocalRepo) GetChangeset(ctx context.Context, id string) (changeset.Changeset, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return changeset.Changeset{}, err
	}
	v, ok := l.state.Changesets[id]
	if !ok {
		return v, fmt.Errorf("unknown changeset %s", id)
	}
	return cloneChangeset(v), nil
}
func (l *LocalRepo) ListChangesets(ctx context.Context, pid string) ([]changeset.Changeset, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return nil, err
	}
	out := []changeset.Changeset{}
	for _, v := range l.state.Changesets {
		if v.ProjectID == pid {
			out = append(out, cloneChangeset(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (l *LocalRepo) List(ctx context.Context, pid string, limit int) ([]events.Event, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if err := l.check(ctx); err != nil {
		return nil, err
	}
	out := []events.Event{}
	for i := len(l.state.Events) - 1; i >= 0 && len(out) < limit; i-- {
		if pid == "" || l.state.Events[i].ProjectID == pid {
			out = append(out, l.state.Events[i])
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	var detached []events.Event
	err = json.Unmarshal(b, &detached)
	return detached, err
}
