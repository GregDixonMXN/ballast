// Package api dispatch: runners register, claim work, and report results.
// The queue is per-runner and in-memory; runners re-register after a
// restart (heartbeats define liveness), so no persistence is needed.
// Assign creates the workspace and enqueues the item; results apply the
// outcome, auto-build the changeset on success, and move the task to
// REVIEW — closing task → code → review without human copy-paste.
package api

import (
	"net/http"
	"sync"
	"time"

	"ballast/internal/auth"
	"ballast/internal/changeset"
	"ballast/internal/events"
	"ballast/internal/task"
	"ballast/internal/workspace"
	"github.com/google/uuid"
)

// RunnerInfo is a registered execution node. Heartbeats keep Online true;
// the control plane never dials out to it.
type RunnerInfo struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	CPU          int       `json:"cpu"`
	MemoryMB     int       `json:"memory_mb"`
	HasDocker    bool      `json:"has_docker"`
	HasGit       bool      `json:"has_git"`
	Online       bool      `json:"online"`
	Capabilities []string  `json:"capabilities,omitempty"`
	LastSeen     time.Time `json:"last_seen"`
}

// WorkItem is one unit of assigned agent work.
type WorkItem struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Adapter     string `json:"adapter,omitempty"`
	TestCommand string `json:"test_command,omitempty"`
	Path        string `json:"path"`
}

// WorkResult reports what the runner did. TestRan=false skips test events.
type WorkResult struct {
	WorkspaceID string `json:"workspace_id"`
	ExitCode    int    `json:"exit_code"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	TestRan     bool   `json:"test_ran,omitempty"`
	TestExit    int    `json:"test_exit,omitempty"`
	TestOutput  string `json:"test_output,omitempty"`
}

// Dispatch tracks runners and their pending queues.
type Dispatch struct {
	mu      sync.Mutex
	runners map[string]*RunnerInfo
	queues  map[string][]WorkItem
}

// NewDispatch builds empty registries.
func NewDispatch() *Dispatch {
	return &Dispatch{runners: map[string]*RunnerInfo{}, queues: map[string][]WorkItem{}}
}

// Register adds a runner and returns its record.
func (d *Dispatch) Register(r *RunnerInfo) *RunnerInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	r.Online = true
	r.LastSeen = time.Now().UTC()
	d.runners[r.ID] = r
	return r
}

// Heartbeat marks liveness; unknown runners are rejected.
func (d *Dispatch) Heartbeat(id string) (*RunnerInfo, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.runners[id]
	if !ok {
		return nil, false
	}
	r.Online = true
	r.LastSeen = time.Now().UTC()
	return r, true
}

// Enqueue appends work for a known runner.
func (d *Dispatch) Enqueue(runnerID string, item WorkItem) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.runners[runnerID]; !ok {
		return false
	}
	d.queues[runnerID] = append(d.queues[runnerID], item)
	return true
}

// Poll pops the oldest item; false when the queue is empty.
func (d *Dispatch) Poll(runnerID string) (WorkItem, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	q := d.queues[runnerID]
	if len(q) == 0 {
		return WorkItem{}, false
	}
	item := q[0]
	d.queues[runnerID] = q[1:]
	return item, true
}

func (s *Server) registerRunner(w http.ResponseWriter, r *http.Request) {
	var in RunnerInfo
	if err := decodeJSON(r, &in); err != nil || in.Hostname == "" {
		writeJSON(w, 400, map[string]string{"error": "hostname required"})
		return
	}
	in.ID = uuid.NewString()
	rec := s.dispatch.Register(&in)
	tok := s.auth.Mint(auth.Identity{ID: rec.ID, Kind: "runner", Roles: []string{"runner"}})
	s.count.Inc("runners_registered")
	writeJSON(w, 201, map[string]any{"runner": rec, "token": tok})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.dispatch.Heartbeat(r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "unknown runner"})
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) pollWork(w http.ResponseWriter, r *http.Request) {
	item, ok := s.dispatch.Poll(r.PathValue("id"))
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) assignTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RunnerID    string `json:"runner_id"`
		Adapter     string `json:"adapter"`
		Prompt      string `json:"prompt"`
		TestCommand string `json:"test_command"`
	}
	if err := decodeJSON(r, &in); err != nil || in.RunnerID == "" {
		writeJSON(w, 400, map[string]string{"error": "runner_id required"})
		return
	}
	if s.Tasks == nil || s.Workspaces == nil {
		writeJSON(w, 501, map[string]string{"error": "services not wired"})
		return
	}
	rawTask, err := s.Tasks.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	t, ok := rawTask.(task.Task)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "task type mismatch"})
		return
	}
	if t.Status == task.Todo {
		if _, err := s.Tasks.Transition(t.ID, string(task.Running)); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
	}
	rawWS, err := s.Workspaces.Create(t.ProjectID, t.ID, "")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	ws, ok := rawWS.(*workspace.Workspace)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "workspace type mismatch"})
		return
	}
	prompt := in.Prompt
	if prompt == "" {
		prompt = "## " + t.Title + "\n" + t.Description
	}
	item := WorkItem{WorkspaceID: ws.ID, ProjectID: t.ProjectID, TaskID: t.ID,
		Title: t.Title, Description: t.Description, Prompt: prompt,
		Adapter: in.Adapter, TestCommand: in.TestCommand, Path: ws.Path}
	if !s.dispatch.Enqueue(in.RunnerID, item) {
		writeJSON(w, 404, map[string]string{"error": "unknown runner"})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New(t.ProjectID, events.ActorHuman, "",
		events.TaskAssigned, t.ID, map[string]any{"runner": in.RunnerID}))
	s.count.Inc("tasks_assigned")
	writeJSON(w, 201, map[string]any{"workspace": ws, "item": item})
}

func (s *Server) reportResults(w http.ResponseWriter, r *http.Request) {
	var in WorkResult
	if err := decodeJSON(r, &in); err != nil || in.WorkspaceID == "" {
		writeJSON(w, 400, map[string]string{"error": "workspace_id required"})
		return
	}
	if s.Workspaces == nil || s.Changesets == nil || s.Tasks == nil {
		writeJSON(w, 501, map[string]string{"error": "services not wired"})
		return
	}
	if _, ok := s.dispatch.Heartbeat(r.PathValue("id")); !ok {
		writeJSON(w, 404, map[string]string{"error": "unknown runner"})
		return
	}
	rawWS, err := s.Workspaces.Get(in.WorkspaceID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	ws, ok := rawWS.(*workspace.Workspace)
	if !ok {
		// Memory and PG repos return values; accept both.
		if v, ok2 := rawWS.(workspace.Workspace); ok2 {
			ws = &v
			ok = true
		}
	}
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "workspace type mismatch"})
		return
	}
	status := workspace.Completed
	if in.ExitCode != 0 {
		status = workspace.Failed
	}
	updated, err := s.Workspaces.SetStatus(ws.ID, string(status))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New(ws.ProjectID, events.ActorAgent, "",
		events.AgentStopped, ws.ID, map[string]any{"exit": in.ExitCode}))
	if in.TestRan {
		typ := events.TestPassed
		if in.TestExit != 0 {
			typ = events.TestFailed
		}
		_ = s.bus.Publish(r.Context(), events.New(ws.ProjectID, events.ActorAgent, "",
			typ, ws.ID, map[string]any{"exit": in.TestExit}))
		s.count.Inc("tests_run")
	}
	resp := map[string]any{"workspace": updated}
	if in.ExitCode == 0 {
		rawCS, err := s.Changesets.Build(ws.ProjectID, ws.TaskID, "", ws.ID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		cs, ok := rawCS.(*changeset.Changeset)
		if !ok {
			writeJSON(w, 500, map[string]string{"error": "changeset type mismatch"})
			return
		}
		_, _ = s.Tasks.Transition(ws.TaskID, string(task.Review))
		_ = s.bus.Publish(r.Context(), events.New(ws.ProjectID, events.ActorAgent, "",
			events.ChangesetCreated, cs.ID, nil))
		_ = s.bus.Publish(r.Context(), events.New(ws.ProjectID, events.ActorSystem, "",
			events.ReviewRequested, cs.ID, nil))
		s.count.Inc("changesets_created")
		resp["changeset_id"] = cs.ID
	} else {
		_, _ = s.Tasks.Transition(ws.TaskID, string(task.Blocked))
	}
	writeJSON(w, 200, resp)
}

func (s *Server) setWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Status == "" {
		writeJSON(w, 400, map[string]string{"error": "status required"})
		return
	}
	if s.Workspaces == nil {
		writeJSON(w, 501, map[string]string{"error": "workspace service not wired"})
		return
	}
	updated, err := s.Workspaces.SetStatus(r.PathValue("id"), in.Status)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, updated)
}
