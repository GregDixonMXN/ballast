// Package overseer is the hardcoded foreman: a periodic server-side sweep
// that prevents conflicts structurally instead of agentically. It works
// off control-plane state (tasks, workspaces, claims, files), so it is
// blind to harnesses — loop, codex, claude, custom CLIs all get the same
// protection. Findings go to the project board + events bus. Advisory
// only: it never blocks, assigns, or integrates.
package overseer

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"ballast/internal/lease"
	"ballast/internal/notes"
)

// Tasks source: subset of the task service the sweep needs.
type TaskLister interface {
	List(projectID string) ([]any, error)
}

// TaskStatus extracts id/title/status from service any-values without
// importing the task package into the sweep core.
type TaskView struct {
	ID     string
	Title  string
	Status string
}

// Adapt converts service values via JSON round-trip (services return
// structs, not maps, and this package must not import them all).
func AdaptTasks(in []any) []TaskView {
	var out []TaskView
	for _, v := range in {
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		s, _ := m["status"].(string)
		out = append(out, TaskView{
			ID:     str(m, "id"),
			Title:  str(m, "title"),
			Status: strings.ToUpper(s),
		})
	}
	return out
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// Workspaces source for file and freshness reads.
type WorkspaceLister interface {
	List(projectID string) ([]any, error)
	ChangedFiles(id string) ([]string, error)
}

// WSView is the sweep's read of one workspace.
type WSView struct {
	ID        string
	TaskID    string
	Status    string
	UpdatedAt time.Time
	Files     []string
}

// AdaptWS converts workspace service values the same way.
func AdaptWS(in []any, files map[string][]string) []WSView {
	var out []WSView
	for _, v := range in {
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		s, _ := m["status"].(string)
		var at time.Time
		if ts, _ := m["updated_at"].(string); ts != "" {
			at, _ = time.Parse(time.RFC3339, ts)
		}
		out = append(out, WSView{
			ID: str(m, "id"), TaskID: str(m, "task_id"),
			Status: strings.ToUpper(s), UpdatedAt: at,
			Files: files[str(m, "id")],
		})
	}
	return out
}

// Bus publishes findings for dashboards.
type Bus interface {
	Publish(ctx context.Context, projectID, typ, ref string, fields map[string]any)
}

// Overseer holds sweep state: which findings were already announced,
// so the board gets each fact once instead of every minute.
type Overseer struct {
	mu        sync.Mutex
	announced map[string]time.Time
}

func New() *Overseer { return &Overseer{announced: map[string]time.Time{}} }

// StuckAfter marks a RUNNING workspace stuck; announces once per day.
const StuckAfter = 15 * time.Minute

// Sweep runs one pass over a project and returns human-readable findings
// (also posted to the board by the caller via Post).
func (o *Overseer) Sweep(projectID string, tasks []TaskView, workspaces []WSView) []string {
	var out []string
	running := map[string][]WSView{}
	for _, w := range workspaces {
		if strings.ToUpper(string(w.Status)) != "RUNNING" {
			continue
		}
		running[w.TaskID] = append(running[w.TaskID], w)
	}
	// 1. Duplicate open tasks by title similarity, plus open tasks
	// duplicating already-DONE work (rework risk).
	open := []TaskView{}
	var done []TaskView
	for _, t := range tasks {
		switch t.Status {
		case "TODO", "RUNNING", "REVIEW", "BLOCKED":
			open = append(open, t)
		case "DONE":
			done = append(done, t)
		}
	}
	for i := 0; i < len(open); i++ {
		for j := i + 1; j < len(open); j++ {
			if similar(open[i].Title, open[j].Title) {
				if o.once("dup:"+pairKey(open[i].ID, open[j].ID), 24*time.Hour) {
					out = append(out, "duplicate tasks: "+short(open[i].ID)+" and "+short(open[j].ID)+
						" share a title ("+open[i].Title+") — one should be deleted")
				}
			}
		}
		for _, d := range done {
			if similar(open[i].Title, d.Title) {
				if o.once("rework:"+open[i].ID+">"+d.ID, 24*time.Hour) {
					out = append(out, "rework risk: open task "+short(open[i].ID)+
						" ("+open[i].Title+") duplicates DONE work in "+short(d.ID))
				}
			}
		}
	}
	// 2. Overlapping scopes among live claims are checked by callers
	// via CheckClaims (leases live outside the snapshot inputs).
	// 3. Overlapping changed files across RUNNING workspaces.
	byFile := map[string][]string{}
	for taskID, wsl := range running {
		seen := map[string]bool{}
		for _, w := range wsl {
			for _, f := range w.Files {
				if !seen[f] {
					seen[f] = true
					byFile[f] = append(byFile[f], taskID)
				}
			}
		}
	}
	for f, owners := range byFile {
		uniq := dedup(owners)
		if len(uniq) > 1 {
			if o.once("file:"+f, 24*time.Hour) {
				out = append(out, "file collision: "+f+" touched by tasks "+strings.Join(shorten(uniq), ", "))
			}
		}
	}
	// 4. Stuck runs: RUNNING with no update for StuckAfter.
	now := time.Now().UTC()
	for taskID, wsl := range running {
		for _, w := range wsl {
			if now.Sub(w.UpdatedAt) > StuckAfter {
				if o.once("stuck:"+w.ID, 24*time.Hour) {
					out = append(out, "stuck run: task "+short(taskID)+" workspace "+short(w.ID)+
						" has shown no progress for "+StuckAfter.String())
				}
			}
		}
	}
	return out
}

// CheckClaims reports live lease overlaps (called with fresh data).
func (o *Overseer) CheckClaims(scopes []lease.Scope) []string {
	var out []string
	for i := 0; i < len(scopes); i++ {
		for j := i + 1; j < len(scopes); j++ {
			a, b := scopes[i], scopes[j]
			if a.OwnerID == b.OwnerID || a.ProjectID != b.ProjectID {
				continue
			}
			if lease.Overlaps(a.Pattern, b.Pattern) {
				if o.once("claim:"+pairKey(a.ID, b.ID), 24*time.Hour) {
					out = append(out, "scope overlap: "+a.Pattern+" ("+short(a.OwnerID)+
						") vs "+b.Pattern+" ("+short(b.OwnerID)+") — smaller task should yield")
				}
			}
		}
	}
	return out
}

// Post writes findings to the board as overseer.
func Post(board *notes.Board, projectID string, findings []string) {
	for _, f := range findings {
		board.Post(projectID, "overseer", f)
	}
}

func (o *Overseer) once(key string, ttl time.Duration) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if at, ok := o.announced[key]; ok && time.Since(at) < ttl {
		return false
	}
	o.announced[key] = time.Now().UTC()
	return true
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func shorten(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = short(id)
	}
	return out
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// similar reports near-duplicate titles by word overlap.
func similar(a, b string) bool {
	wa, wb := wordSet(a), wordSet(b)
	if len(wa) < 3 || len(wb) < 3 {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	if union == 0 {
		return false
	}
	return float64(inter)/float64(union) > 0.6
}

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if len(w) > 2 {
			out[w] = true
		}
	}
	return out
}
