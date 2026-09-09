// Package conflict detects coordination hazards before they destroy work.
// V1 is deliberately syntactic: same-file overlap, same-region (hunk)
// overlap, stale base, git test-merge conflicts, and lease-scope overlap.
// Semantic types (symbol, contract, schema, dependency) are declared now
// as future enum values so stored data never needs migration.
package conflict

import (
	"context"
	"time"

	"ballast/internal/git"
	"github.com/google/uuid"
)

// Kind of conflict detected.
type Kind string

const (
	SameFile     Kind = "SAME_FILE"
	SameRegion   Kind = "SAME_REGION"
	StaleBase    Kind = "STALE_BASE"
	Integrate    Kind = "INTEGRATE_CONFLICT"
	ScopeOverlap Kind = "SCOPE_OVERLAP"
	Symbol       Kind = "SYMBOL"       // future
	APIContract  Kind = "API_CONTRACT" // future
	Schema       Kind = "SCHEMA"       // future
	Dependency   Kind = "DEPENDENCY"   // future
	Semantic     Kind = "SEMANTIC"     // future
)

// Severity guides the UI, never auto-resolves.
type Severity string

const (
	Info     Severity = "INFO"
	Warning  Severity = "WARNING"
	Blocking Severity = "BLOCKING"
)

// Status of a conflict record.
type Status string

const (
	Open     Status = "OPEN"
	Acked    Status = "ACKED"
	Resolved Status = "RESOLVED"
)

// Conflict is a first-class coordination object.
type Conflict struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	WorkspaceA    string    `json:"workspace_a"`
	WorkspaceB    string    `json:"workspace_b,omitempty"`
	Kind          Kind      `json:"kind"`
	Severity      Severity  `json:"severity"`
	AffectedFiles []string  `json:"affected_files"`
	Reason        string    `json:"reason"`
	Action        string    `json:"action"`
	Status        Status    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

func newConflict(projectID, a, b string, kind Kind, sev Severity, files []string, reason, action string) Conflict {
	return Conflict{ID: uuid.NewString(), ProjectID: projectID, WorkspaceA: a,
		WorkspaceB: b, Kind: kind, Severity: sev, AffectedFiles: files,
		Reason: reason, Action: action, Status: Open, CreatedAt: time.Now().UTC()}
}

// rangesOverlap reports [s1,e1) ∩ [s2,e2) ≠ ∅.
func rangesOverlap(s1, e1, s2, e2 int) bool { return s1 < e2 && s2 < e1 }

// FilesChanged abstracts a workspace snapshot for pure detection.
type FilesChanged struct {
	WorkspaceID string
	Files       []string
	Hunks       map[string][][2]int
	Base        string
}

// DetectFiles returns a SAME_FILE conflict when active workspaces share paths.
func DetectFiles(projectID string, snaps []FilesChanged) []Conflict {
	seen := map[string]string{}
	var out []Conflict
	for _, s := range snaps {
		for _, f := range s.Files {
			if other, ok := seen[f]; ok && other != s.WorkspaceID {
				out = append(out, newConflict(projectID, other, s.WorkspaceID,
					SameFile, Warning, []string{f},
					"two active workspaces modify "+f,
					"coordinate owners before integrating either changeset"))
			} else {
				seen[f] = s.WorkspaceID
			}
		}
	}
	return out
}

// DetectRegions refines file overlap to hunk ranges when available.
func DetectRegions(projectID string, snaps []FilesChanged) []Conflict {
	byFile := map[string]map[string][][2]int{}
	owner := map[string]map[string]string{}
	var out []Conflict
	for _, s := range snaps {
		for f, hs := range s.Hunks {
			if byFile[f] == nil {
				byFile[f] = map[string][][2]int{}
				owner[f] = map[string]string{}
			}
			for id, ranges := range byFile[f] {
				for _, r1 := range ranges {
					for _, r2 := range hs {
						if rangesOverlap(r1[0], r1[1], r2[0], r2[1]) {
							out = append(out, newConflict(projectID, id, s.WorkspaceID,
								SameRegion, Blocking, []string{f},
								"overlapping changed regions in "+f,
								"rebase one workspace onto the other's integration before merge"))
						}
					}
				}
				_ = owner
			}
			byFile[f][s.WorkspaceID] = append(byFile[f][s.WorkspaceID], hs...)
		}
	}
	return out
}

// DetectStale flags workspaces whose base differs from canonical head.
func DetectStale(projectID, workspaceID, base, canonical string) *Conflict {
	if base == canonical {
		return nil
	}
	c := newConflict(projectID, workspaceID, "", StaleBase, Warning, nil,
		"workspace base "+short(base)+" is behind canonical "+short(canonical),
		"rebase workspace onto new head and re-run tests before integration")
	return &c
}

// LiveHunks loads hunk maps for real worktrees (region detection).
func LiveHunks(ctx context.Context, path, base string) map[string][][2]int {
	h, err := git.Hunks(ctx, path, base)
	if err != nil {
		return nil
	}
	return h
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
