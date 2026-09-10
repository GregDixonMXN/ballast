package api

import (
	"net/http"

	"ballast/internal/changeset"
	"ballast/internal/events"
	"ballast/internal/services"
)

// Handlers below are thin: services are injected in production wiring
// (cmd/server). Each emits the matching typed event on success.

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string `json:"name"`
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.Repo == "" {
		writeJSON(w, 400, map[string]string{"error": "name and repo required"})
		return
	}
	if in.Branch == "" {
		in.Branch = "main"
	}
	if s.Projects == nil {
		writeJSON(w, 501, map[string]string{"error": "project service not wired"})
		return
	}
	p, err := s.Projects.Create(in.Name, in.Repo, in.Branch)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New("", events.ActorHuman, "", events.ProjectCreated, "", map[string]any{"name": in.Name}))
	s.count.Inc("projects_created")
	writeJSON(w, 201, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	if s.Projects == nil {
		writeJSON(w, 501, map[string]string{"error": "project service not wired"})
		return
	}
	p, err := s.Projects.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Scopes      []string `json:"scopes"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Title == "" {
		writeJSON(w, 400, map[string]string{"error": "title required"})
		return
	}
	if s.Tasks == nil {
		writeJSON(w, 501, map[string]string{"error": "task service not wired"})
		return
	}
	t, err := s.Tasks.Create(r.PathValue("id"), in.Title, in.Description, in.Scopes)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New(r.PathValue("id"), events.ActorHuman, "", events.TaskCreated, "", map[string]any{"title": in.Title}))
	s.count.Inc("tasks_created")
	writeJSON(w, 201, t)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	if s.Tasks == nil {
		writeJSON(w, 501, map[string]string{"error": "task service not wired"})
		return
	}
	ts, err := s.Tasks.List(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, ts)
}

func (s *Server) transitionTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	if err := decodeJSON(r, &in); err != nil || in.To == "" {
		writeJSON(w, 400, map[string]string{"error": "to required"})
		return
	}
	if s.Tasks == nil {
		writeJSON(w, 501, map[string]string{"error": "task service not wired"})
		return
	}
	t, err := s.Tasks.Transition(r.PathValue("id"), in.To)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, t)
}

func taskWriteError(w http.ResponseWriter, err error) {
	if err == services.ErrTaskRunning {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 400, map[string]string{"error": err.Error()})
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Scopes      []string `json:"scopes"`
		HasScopes   bool     `json:"has_scopes"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	if s.Tasks == nil {
		writeJSON(w, 501, map[string]string{"error": "task service not wired"})
		return
	}
	var scopes []string
	if in.HasScopes {
		scopes = in.Scopes
	}
	t, err := s.Tasks.Update(r.PathValue("id"), in.Title, in.Description, scopes)
	if err != nil {
		taskWriteError(w, err)
		return
	}
	_ = s.bus.Publish(r.Context(), events.New("", events.ActorHuman, "", events.TaskUpdated, r.PathValue("id"), nil))
	writeJSON(w, 200, t)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	if s.Tasks == nil {
		writeJSON(w, 501, map[string]string{"error": "task service not wired"})
		return
	}
	if err := s.Tasks.Delete(r.PathValue("id")); err != nil {
		if err == services.ErrTaskRunning {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New("", events.ActorHuman, "", events.TaskDeleted, r.PathValue("id"), nil))
	writeJSON(w, 200, map[string]string{"deleted": r.PathValue("id")})
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeJSON(r, &in); err != nil || in.TaskID == "" {
		writeJSON(w, 400, map[string]string{"error": "task_id required"})
		return
	}
	if s.Workspaces == nil {
		writeJSON(w, 501, map[string]string{"error": "workspace service not wired"})
		return
	}
	ws, err := s.Workspaces.Create(r.PathValue("id"), in.TaskID, "")
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New(r.PathValue("id"), events.ActorSystem, "", events.WorkspaceCreated, "", nil))
	s.count.Inc("workspaces_created")
	writeJSON(w, 201, ws)
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	if s.Workspaces == nil {
		writeJSON(w, 501, map[string]string{"error": "workspace service not wired"})
		return
	}
	list, err := s.Workspaces.List(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) wsFiles(w http.ResponseWriter, r *http.Request) {
	if s.Workspaces == nil {
		writeJSON(w, 501, map[string]string{"error": "workspace service not wired"})
		return
	}
	files, err := s.Workspaces.ChangedFiles(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"files": files})
}

func (s *Server) wsDiff(w http.ResponseWriter, r *http.Request) {
	if s.Workspaces == nil {
		writeJSON(w, 501, map[string]string{"error": "workspace service not wired"})
		return
	}
	diff, err := s.Workspaces.Diff(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"diff": diff})
}

func (s *Server) buildChangeset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID string `json:"project_id"`
		TaskID    string `json:"task_id"`
		AgentID   string `json:"agent_id"`
	}
	_ = decodeJSON(r, &in)
	if s.Changesets == nil {
		writeJSON(w, 501, map[string]string{"error": "changeset service not wired"})
		return
	}
	cs, err := s.Changesets.Build(in.ProjectID, in.TaskID, in.AgentID, r.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New(in.ProjectID, events.ActorAgent, in.AgentID, events.ChangesetCreated, "", nil))
	s.count.Inc("changesets_created")
	writeJSON(w, 201, cs)
}

func (s *Server) getChangeset(w http.ResponseWriter, r *http.Request) {
	if s.Changesets == nil {
		writeJSON(w, 501, map[string]string{"error": "changeset service not wired"})
		return
	}
	cs, err := s.Changesets.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, cs)
}

func (s *Server) decideChangeset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"` // approve | reject
		Actor    string `json:"actor"`
	}
	if err := decodeJSON(r, &in); err != nil || (in.Decision != "approve" && in.Decision != "reject") {
		writeJSON(w, 400, map[string]string{"error": "decision must be approve|reject"})
		return
	}
	if s.Changesets == nil {
		writeJSON(w, 501, map[string]string{"error": "changeset service not wired"})
		return
	}
	ident, _ := s.auth.Parse(r.Header.Get("Authorization"))
	in.Actor = ident.ID
	cs, err := s.Changesets.Decide(r.PathValue("id"), in.Decision, in.Actor)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	typ := events.ApprovalGranted
	if in.Decision == "reject" {
		typ = events.ApprovalRejected
	}
	_ = s.bus.Publish(r.Context(), events.New("", events.ActorHuman, in.Actor, typ, r.PathValue("id"), nil))
	writeJSON(w, 200, cs)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	if s.Projects == nil {
		writeJSON(w, 503, map[string]string{"error": "projects unavailable"})
		return
	}
	out, err := s.Projects.List()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	if s.Tasks == nil {
		writeJSON(w, 503, map[string]string{"error": "tasks unavailable"})
		return
	}
	out, err := s.Tasks.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) listChangesets(w http.ResponseWriter, r *http.Request) {
	if s.Changesets == nil {
		writeJSON(w, 503, map[string]string{"error": "changesets unavailable"})
		return
	}
	out, err := s.Changesets.List(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if out == nil {
		out = []changeset.Changeset{}
	}
	writeJSON(w, 200, out)
}
func (s *Server) listRunners(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.dispatch.List())
}
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	if s.EventStore == nil {
		writeJSON(w, 200, []events.Event{})
		return
	}
	out, err := s.EventStore.List(r.Context(), r.URL.Query().Get("project"), 200)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if out == nil {
		out = []events.Event{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.Workspaces == nil {
		writeJSON(w, 503, map[string]string{"error": "workspaces unavailable"})
		return
	}
	out, err := s.Workspaces.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, out)
}
