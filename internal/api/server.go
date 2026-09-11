// Package api is the control-plane HTTP boundary: REST for state, SSE for
// live events. The web UI, CLI, and runners are all thin clients of this
// API. Auth is bearer-token MVP (see internal/auth); every mutating route
// records an audit entry and emits a typed event.
package api

import (
	"ballast/internal/project"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"ballast/internal/auth"
	"ballast/internal/changeset"
	"ballast/internal/events"
	"ballast/internal/lease"
	"ballast/internal/notes"
	"ballast/internal/telemetry"
)

// Server wires handlers to domain services.
type Server struct {
	mutation   sync.Mutex
	EventStore events.Store
	mux        *http.ServeMux
	bus        events.Bus
	auth       *auth.Tokens
	perms      auth.Authorizer
	count      *telemetry.Counters
	dispatch   *Dispatch
	// Services are interfaces so Postgres/memory swap without handler edits.
	Projects   ProjectService
	Tasks      TaskService
	Workspaces WorkspaceService
	Changesets ChangesetService
	// Coordination primitives live in memory on the server: leases are
	// cooperative scope claims, notes are the project blackboard. Both
	// are advisory — the changeset conflict check stays authoritative.
	Leases *lease.Manager
	Notes  *notes.Board
}

type ProjectService interface {
	List() ([]project.Project, error)
	Create(name, repo, branch string) (any, error)
	CreateWithSpec(name, repo, branch, testCommand, standards string) (any, error)
	Get(id string) (any, error)
	UpdateSpec(id, testCommand, standards string) (any, error)
	Head(id string) (string, error)
	SetHead(id, sha string) error
}
type TaskService interface {
	Create(projectID, title, desc string, scopes []string) (any, error)
	CreateWithDeps(projectID, title, desc string, scopes, depends []string) (any, error)
	CreateFull(projectID, title, desc string, scopes, depends []string, gate string) (any, error)
	Get(id string) (any, error)
	List(projectID string) ([]any, error)
	Ready(projectID string) ([]any, error)
	Transition(id, to string) (any, error)
	Update(id, title, desc string, scopes []string) (any, error)
	Delete(id string) error
}
type WorkspaceService interface {
	Create(projectID, taskID, repo string) (any, error)
	Get(id string) (any, error)
	SetStatus(id, status string) (any, error)
	SetReport(id, status string, exit int, stdout, stderr string, testExit int) (any, error)
	List(projectID string) ([]any, error)
	ChangedFiles(id string) ([]string, error)
	Diff(id string) (string, error)
}
type ChangesetService interface {
	Build(projectID, taskID, agentID, wsID string) (any, error)
	Get(id string) (any, error)
	Decide(id, decision, actor string) (any, error)
	List(projectID string) ([]changeset.Changeset, error)
	Mark(id string, status changeset.Status) (changeset.Changeset, error)
}

// New builds routes. Health/readiness stay unauthenticated for probes.
func New(bus events.Bus, toks *auth.Tokens) *Server {
	s := &Server{mux: http.NewServeMux(), bus: bus, auth: toks,
		perms: auth.LocalAuthorizer{}, count: telemetry.NewCounters(),
		dispatch: NewDispatch(), Leases: lease.New(), Notes: notes.New()}
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /readyz", s.health)
	s.mux.HandleFunc("GET /metrics", s.requireAuth(s.metrics))
	s.mux.HandleFunc("GET /activity", s.requireAuth(s.activity))
	s.mux.HandleFunc("GET /projects", s.requireAuth(s.listProjects))
	s.mux.HandleFunc("GET /runners", s.requireAuth(s.listRunners))
	s.mux.HandleFunc("GET /tasks/{id}", s.requireAuth(s.getTask))
	s.mux.HandleFunc("GET /projects/{id}/changesets", s.requireAuth(s.listChangesets))
	s.mux.HandleFunc("GET /events", s.requireAuth(s.stream)) // ?project=
	s.mux.HandleFunc("POST /projects", s.requireAuth(s.createProject))
	s.mux.HandleFunc("GET /projects/{id}", s.requireAuth(s.getProject))
	s.mux.HandleFunc("PATCH /projects/{id}", s.requireAuth(s.updateProject))
	s.mux.HandleFunc("POST /projects/{id}/tasks", s.requireAuth(s.createTask))
	s.mux.HandleFunc("GET /projects/{id}/tasks", s.requireAuth(s.listTasks))
	s.mux.HandleFunc("GET /projects/{id}/ready", s.requireAuth(s.readyTasks))
	s.mux.HandleFunc("POST /tasks/{id}/transition", s.requireAuth(s.transitionTask))
	s.mux.HandleFunc("PATCH /tasks/{id}", s.requireAuth(s.updateTask))
	s.mux.HandleFunc("DELETE /tasks/{id}", s.requireAuth(s.deleteTask))
	s.mux.HandleFunc("POST /projects/{id}/workspaces", s.requireAuth(s.createWorkspace))
	s.mux.HandleFunc("GET /projects/{id}/workspaces", s.requireAuth(s.listWorkspaces))
	s.mux.HandleFunc("GET /workspaces/{id}", s.requireAuth(s.getWorkspace))
	s.mux.HandleFunc("GET /workspaces/{id}/files", s.requireAuth(s.wsFiles))
	s.mux.HandleFunc("GET /workspaces/{id}/diff", s.requireAuth(s.wsDiff))
	s.mux.HandleFunc("POST /workspaces/{id}/changesets", s.requireAuth(s.buildChangeset))
	s.mux.HandleFunc("GET /changesets/{id}", s.requireAuth(s.getChangeset))
	s.mux.HandleFunc("POST /changesets/{id}/decision", s.requireAuth(s.decideChangeset))
	s.mux.HandleFunc("POST /changesets/{id}/integrate", s.requireAuth(s.integrate))
	s.mux.HandleFunc("POST /runners", s.requireAuth(s.registerRunner))
	s.mux.HandleFunc("POST /runners/{id}/heartbeat", s.requireAuth(s.heartbeat))
	s.mux.HandleFunc("GET /runners/{id}/work", s.requireAuth(s.pollWork))
	s.mux.HandleFunc("POST /runners/{id}/results", s.requireAuth(s.reportResults))
	s.mux.HandleFunc("POST /tasks/{id}/assign", s.requireAuth(s.assignTask))
	s.mux.HandleFunc("POST /workspaces/{id}/status", s.requireAuth(s.setWorkspaceStatus))
	s.mux.HandleFunc("POST /workspaces/{id}/claim", s.requireAuth(s.claimScope))
	s.mux.HandleFunc("GET /projects/{id}/notes", s.requireAuth(s.listNotes))
	s.mux.HandleFunc("POST /projects/{id}/notes", s.requireAuth(s.postNote))
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
	_, _ = w.Write([]byte(": connected\n\n"))
	if fl != nil {
		fl.Flush()
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			if fl != nil {
				fl.Flush()
			}
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
		ident, err := s.auth.Parse(r.Header.Get("Authorization"))
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if ident.Kind == "runner" {
			ownRoute := strings.HasPrefix(r.URL.Path, "/runners/"+ident.ID+"/")
			wsRoute := strings.HasPrefix(r.URL.Path, "/workspaces/") &&
				(strings.HasSuffix(r.URL.Path, "/status") || strings.HasSuffix(r.URL.Path, "/claim"))
			notesRoute := strings.Contains(r.URL.Path, "/notes")
			// Foreman reads: a runner may list project state (tasks,
			// workspaces, changesets) to supervise siblings. Reads only;
			// mutations stay gated above.
			readRoute := r.Method == "GET" && (strings.Contains(r.URL.Path, "/tasks") ||
				strings.Contains(r.URL.Path, "/workspaces") ||
				strings.Contains(r.URL.Path, "/changesets"))
			if !ownRoute && !wsRoute && !notesRoute && !readRoute {
				writeJSON(w, 403, map[string]string{"error": "operator capability required"})
				return
			}
			if wsRoute && !s.dispatch.Owns(ident.ID, r.PathValue("id")) {
				writeJSON(w, 403, map[string]string{"error": "workspace not assigned to runner"})
				return
			}
			if notesRoute && !s.dispatch.OwnsProject(ident.ID, r.PathValue("id")) {
				writeJSON(w, 403, map[string]string{"error": "no assigned work in project"})
				return
			}
		} else if r.Method != "GET" && !s.perms.Can(r.Context(), ident, "", "write") {
			writeJSON(w, 403, map[string]string{"error": "write capability required"})
			return
		}
		if r.Method != "GET" {
			s.mutation.Lock()
			defer s.mutation.Unlock()
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
