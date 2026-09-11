package api

import (
	"fmt"
	"net/http"
	"strings"

	"ballast/internal/workspace"
)

// claimScope records a cooperative scope claim for a workspace and
// reports overlapping claims by others. Advisory only: overlapping
// claims warn, they never block. Runners may claim their own
// workspaces; operators may claim anything.
func (s *Server) claimScope(w http.ResponseWriter, r *http.Request) {
	if s.Leases == nil {
		writeJSON(w, 501, map[string]string{"error": "leases not wired"})
		return
	}
	var in struct {
		Pattern string `json:"pattern"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Pattern) == "" {
		writeJSON(w, 400, map[string]string{"error": "pattern required"})
		return
	}
	ws, err := s.workspaceRecord(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	scope, overlaps := s.Leases.Acquire(ws.ProjectID, ws.ID, strings.TrimSpace(in.Pattern))
	mine := []map[string]string{}
	for _, o := range overlaps {
		mine = append(mine, map[string]string{"pattern": o.Pattern, "owner": o.OwnerID})
	}
	writeJSON(w, 200, map[string]any{"scope": scope, "overlaps": mine})
}

// listNotes returns the project blackboard, newest first.
func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	if s.Notes == nil {
		writeJSON(w, 501, map[string]string{"error": "notes not wired"})
		return
	}
	writeJSON(w, 200, s.Notes.List(r.PathValue("id")))
}

// postNote appends to the project blackboard. Author defaults to the
// caller's workspace when a runner posts; operators pass author or
// default to "operator".
func (s *Server) postNote(w http.ResponseWriter, r *http.Request) {
	if s.Notes == nil {
		writeJSON(w, 501, map[string]string{"error": "notes not wired"})
		return
	}
	var in struct {
		Text      string `json:"text"`
		Author    string `json:"author"`
		Workspace string `json:"workspace_id"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeJSON(w, 400, map[string]string{"error": "text required"})
		return
	}
	author := strings.TrimSpace(in.Author)
	if author == "" {
		author = "operator"
	}
	if in.Workspace != "" {
		ws, err := s.workspaceRecord(in.Workspace)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		if ws.ProjectID != r.PathValue("id") {
			writeJSON(w, 400, map[string]string{"error": "workspace not in project"})
			return
		}
		author = in.Workspace
	}
	writeJSON(w, 201, s.Notes.Post(r.PathValue("id"), author, strings.TrimSpace(in.Text)))
}

// workspaceRecord loads a workspace as a struct for coordination reads.
func (s *Server) workspaceRecord(id string) (workspace.Workspace, error) {
	if s.Workspaces == nil {
		return workspace.Workspace{}, fmt.Errorf("services not wired")
	}
	raw, err := s.Workspaces.Get(id)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if ws, ok := raw.(workspace.Workspace); ok {
		return ws, nil
	}
	if p, ok := raw.(*workspace.Workspace); ok && p != nil {
		return *p, nil
	}
	return workspace.Workspace{}, fmt.Errorf("workspace type mismatch")
}

// promptContext builds the shared-awareness preamble for an assignment:
// active sibling claims and recent board notes. Capped so coordination
// never eats the context it is trying to protect.
func (s *Server) promptContext(projectID, excludeOwner string) string {
	var b strings.Builder
	if s.Leases != nil {
		var sibs []string
		for _, l := range s.Leases.Active(projectID) {
			if l.OwnerID == excludeOwner {
				continue
			}
			sibs = append(sibs, l.Pattern+" (task "+shortOwner(l.OwnerID)+")")
		}
		if len(sibs) > 0 {
			if len(sibs) > 8 {
				sibs = sibs[:8]
			}
			b.WriteString("SIBLING AGENTS are working in: " + strings.Join(sibs, "; ") + ".\n" +
				"Avoid editing those paths. If you must touch shared files (workspace manifests, shared types), say so in your summary.\n")
		}
	}
	if s.Notes != nil {
		ns := s.Notes.List(projectID)
		if len(ns) > 0 {
			if len(ns) > 6 {
				ns = ns[:6]
			}
			b.WriteString("PROJECT BOARD (newest first):\n")
			for _, n := range ns {
				b.WriteString("- [" + shortOwner(n.Author) + "] " + oneLine(n.Text, 220) + "\n")
			}
		}
	}
	return b.String()
}

func shortOwner(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func oneLine(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
