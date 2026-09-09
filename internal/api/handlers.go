package api

import (
	"net/http"

	"ballast/internal/events"
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
