// Integrate endpoint: approved changesets merge through test-merge
// worktrees only. The merge capability (admin/developer) gates it —
// runner tokens are rejected, so agents can never merge themselves.
// After a successful merge, sibling changesets on the old base are
// revalidated immediately: still-applies → NEEDS_REBASE, otherwise
// CONFLICTED with a conflict.detected event. Nothing force-merges.
package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/GregDixonMXN/ballast/internal/changeset"
	"github.com/GregDixonMXN/ballast/internal/events"
	"github.com/GregDixonMXN/ballast/internal/git"
	"github.com/GregDixonMXN/ballast/internal/integration"
	"github.com/GregDixonMXN/ballast/internal/jev"
	"github.com/GregDixonMXN/ballast/internal/project"
)

func (s *Server) integrate(w http.ResponseWriter, r *http.Request) {
	ident, err := s.auth.Parse(r.Header.Get("Authorization"))
	if err != nil || !s.perms.Can(r.Context(), ident, "", "merge") {
		writeJSON(w, 403, map[string]string{"error": "merge capability required"})
		return
	}
	if s.Changesets == nil || s.Projects == nil {
		writeJSON(w, 501, map[string]string{"error": "services not wired"})
		return
	}
	rawCS, err := s.Changesets.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	cs, ok := rawCS.(changeset.Changeset)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "changeset type mismatch"})
		return
	}
	if cs.Status != changeset.Approved && cs.Status != changeset.NeedsRebase {
		writeJSON(w, 409, map[string]string{"error": "only APPROVED changesets integrate (status " + string(cs.Status) + ")"})
		return
	}
	rawP, err := s.Projects.Get(cs.ProjectID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	p, ok := rawP.(project.Project)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "project type mismatch"})
		return
	}
	res, err := integration.Integrate(r.Context(), p.RepoPath, p.Branch, &cs, os.TempDir())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if _, err := s.Changesets.Mark(cs.ID, cs.Status); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]any{"merged": res.Merged, "new_head": res.NewHead,
		"reason": res.Reason, "conflict": res.Conflict, "status": string(cs.Status)}
	if !res.Merged {
		writeJSON(w, 200, resp)
		return
	}
	if err := s.Projects.SetHead(p.ID, res.NewHead); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_ = s.bus.Publish(r.Context(), events.New(p.ID, events.ActorHuman, ident.ID,
		events.MergeCompleted, cs.ID, map[string]any{"head": res.NewHead}))
	s.count.Inc("integrations")
	if s.Tasks != nil {
		_, _ = s.Tasks.Transition(cs.TaskID, "DONE")
	}
	revalidated := s.revalidateSiblings(r, p, cs.ID, res.NewHead)
	resp["revalidated"] = revalidated
	writeJSON(w, 200, resp)
}

// revalidateSiblings marks every other live changeset on the old base.
// Returns counts by outcome for the response.
func (s *Server) revalidateSiblings(r *http.Request, p project.Project, mergedID, newHead string) map[string]int {
	out := map[string]int{"rebase": 0, "conflicted": 0}
	list, err := s.Changesets.List(p.ID)
	if err != nil {
		return out
	}
	for _, sib := range list {
		if sib.ID == mergedID {
			continue
		}
		if sib.Status != changeset.InReview && sib.Status != changeset.Approved {
			continue
		}
		if sib.Base == newHead {
			continue
		}
		if integration.Applies(r.Context(), p.RepoPath, newHead, sib.Diff, os.TempDir()) {
			_, _ = s.Changesets.Mark(sib.ID, changeset.NeedsRebase)
			out["rebase"]++
		} else if s.jevRebasable(r, p.RepoPath, mergedID, sib) {
			// Semantic triage: mechanical drift, not a real clash. The
			// sibling's intent survives a rebase — don't dead-end it.
			_, _ = s.Changesets.Mark(sib.ID, changeset.NeedsRebase)
			out["rebase"]++
		} else {
			_, _ = s.Changesets.Mark(sib.ID, changeset.Conflicted)
			_ = s.bus.Publish(r.Context(), events.New(p.ID, events.ActorSystem, "",
				events.ConflictDetected, sib.ID,
				map[string]any{"files": sib.Files, "reason": "stale base after " + shortHead(newHead)}))
			s.count.Inc("conflicts_detected")
			out["conflicted"]++
		}
	}
	return out
}

// jevRebasable asks whether a sibling that no longer applies is only
// mechanical drift. True means NEEDS_REBASE instead of CONFLICTED. Any
// failure — flag off, no key, judge error, torn judgment — returns false,
// keeping today's fail-toward-human verdict.
func (s *Server) jevRebasable(r *http.Request, repoPath, mergedID string, sib changeset.Changeset) bool {
	if !jev.Enabled() {
		return false
	}
	rawMerged, err := s.Changesets.Get(mergedID)
	if err != nil {
		return false
	}
	merged, ok := rawMerged.(changeset.Changeset)
	if !ok {
		return false
	}
	// Base content both changes started from: the context that lets the
	// model tell drift from clash. Best effort — unreadable files are
	// simply absent from the judgment.
	base := map[string]string{}
	for i, f := range sib.Files {
		if i >= 3 {
			break
		}
		content, err := git.Show(r.Context(), repoPath, sib.Base+":"+f)
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		base[f] = content
	}
	d, err := jev.JudgeSibling(jev.Sibling{
		MergedDiff:   merged.Diff,
		MergedFiles:  merged.Files,
		SiblingDiff:  sib.Diff,
		SiblingFiles: sib.Files,
		BaseFiles:    base,
	})
	if err != nil {
		return false
	}
	return d.Choice == "rebasable" && d.Confidence >= 0.5
}

func shortHead(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
