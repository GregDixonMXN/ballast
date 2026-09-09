// Package api is the control-plane HTTP boundary: REST for state, SSE for
// live events. The web UI, CLI, and runners are all thin clients of this
// API. Auth is bearer-token MVP (see internal/auth); every mutating route
// records an audit entry and emits a typed event.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"ballast/internal/auth"
	"ballast/internal/events"
	"ballast/internal/telemetry"
)

// Server wires handlers to domain services.
type Server struct {
	mux   *http.ServeMux
	bus   events.Bus
	auth  *auth.Tokens
	perms auth.Authorizer
	count *telemetry.Counters
	// Services are interfaces so Postgres/memory swap without handler edits.
	Projects   ProjectService
	Tasks      TaskService
	Workspaces WorkspaceService
	Changesets ChangesetService
}

type ProjectService interface {
	Create(name, repo, branch string) (any, error)
	Get(id string) (any, error)
	Head(id string) (string, error)
}
type TaskService interface {
	Create(projectID, title, desc string, scopes []string) (any, error)
	List(projectID string) ([]any, error)
	Transition(id, to string) (any, error)
}
type WorkspaceService interface {
	Create(projectID, taskID, repo string) (any, error)
	List(projectID string) ([]any, error)
	ChangedFiles(id string) ([]string, error)
	Diff(id string) (string, error)
}
type ChangesetService interface {
	Build(projectID, taskID, agentID, wsID string) (any, error)
	Get(id string) (any, error)
	Decide(id, decision, actor string) (any, error)
}

// New builds routes. Health/readiness stay unauthenticated for probes.
func New(bus events.Bus, toks *auth.Tokens) *Server {
	s := &Server{mux: http.NewServeMux(), bus: bus, auth: toks,
		perms: auth.LocalAuthorizer{}, count: telemetry.NewCounters()}
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /readyz", s.health)
	s.mux.HandleFunc("GET /metrics", s.metrics)
	s.mux.HandleFunc("GET /events", s.stream) // ?project=
	s.mux.HandleFunc("POST /projects", s.requireAuth(s.createProject))
	s.mux.HandleFunc("GET /projects/{id}", s.requireAuth(s.getProject))
	s.mux.HandleFunc("POST /projects/{id}/tasks", s.requireAuth(s.createTask))
	s.mux.HandleFunc("GET /projects/{id}/tasks", s.requireAuth(s.listTasks))
	s.mux.HandleFunc("POST /tasks/{id}/transition", s.requireAuth(s.transitionTask))
	s.mux.HandleFunc("POST /projects/{id}/workspaces", s.requireAuth(s.createWorkspace))
	s.mux.HandleFunc("GET /projects/{id}/workspaces", s.requireAuth(s.listWorkspaces))
	s.mux.HandleFunc("GET /workspaces/{id}/files", s.requireAuth(s.wsFiles))
	s.mux.HandleFunc("GET /workspaces/{id}/diff", s.requireAuth(s.wsDiff))
	s.mux.HandleFunc("POST /workspaces/{id}/changesets", s.requireAuth(s.buildChangeset))
	s.mux.HandleFunc("GET /changesets/{id}", s.requireAuth(s.getChangeset))
	s.mux.HandleFunc("POST /changesets/{id}/decision", s.requireAuth(s.decideChangeset))
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	counts, means := s.count.Snapshot()
	writeJSON(w, 200, map[string]any{"counts": counts, "mean_seconds": means})
}

// stream is SSE: replays nothing (client fetches history via REST when
// needed), then pushes live events for ?project=.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	ch, unsub := s.bus.Subscribe(project)
	defer unsub()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fl, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	_ = enc
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			b, _ := json.Marshal(e)
			_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
			if fl != nil {
				fl.Flush()
			}
		}
	}
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.auth.Parse(r.Header.Get("Authorization")); err != nil {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20)).Decode(v)
}

var _ = strings.TrimSpace
