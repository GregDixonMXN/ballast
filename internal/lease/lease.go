// Package lease implements cooperative scope claims. A task/workspace claims
// path scopes (MVP: glob-ish prefixes like "src/auth/*"). Overlapping
// claims warn — they never hard-block. Semantic scopes (symbols, schemas)
// reuse this record with a different ScopeKind later.
package lease

import (
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Scope is a claimed region of the codebase.
type Scope struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	OwnerID   string    `json:"owner_id"` // task or workspace id
	Pattern   string    `json:"pattern"`  // e.g. "src/auth/*", "web/**"
	Active    bool      `json:"active"`
	Acquired  time.Time `json:"acquired_at"`
}

// Manager tracks active leases in memory; the API persists them via Store.
type Manager struct {
	mu sync.Mutex
	ls []Scope
}

// Acquire records a claim and returns overlapping active claims owned by others.
func (m *Manager) Acquire(projectID, ownerID, pattern string) (Scope, []Scope) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Scope{ID: uuid.NewString(), ProjectID: projectID, OwnerID: ownerID, Pattern: pattern, Active: true, Acquired: time.Now().UTC()}
	var overlaps []Scope
	for _, o := range m.ls {
		if !o.Active || o.ProjectID != projectID || o.OwnerID == ownerID {
			continue
		}
		if Overlaps(o.Pattern, pattern) {
			overlaps = append(overlaps, o)
		}
	}
	m.ls = append(m.ls, s)
	return s, overlaps
}

// Release deactivates all leases for an owner.
func (m *Manager) Release(ownerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.ls {
		if m.ls[i].OwnerID == ownerID {
			m.ls[i].Active = false
		}
	}
}

// Overlaps reports whether two path patterns can cover the same file.
// Supports exact, prefix/*, and ** suffix forms. Conservative: unknown
// forms that share a first segment count as overlapping.
func Overlaps(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b || a == "**" || b == "**" {
		return true
	}
	pa := strings.TrimSuffix(strings.TrimSuffix(a, "/**"), "/*")
	pb := strings.TrimSuffix(strings.TrimSuffix(b, "/**"), "/*")
	if pa == pb {
		return true
	}
	if strings.HasPrefix(pa, pb+"/") || strings.HasPrefix(pb, pa+"/") {
		return true
	}
	// file-vs-dir: "src/api/handler.go" vs "src/api/*"
	da, db := path.Dir(a), path.Dir(b)
	if da == db || strings.HasPrefix(da, db+"/") || strings.HasPrefix(db, da+"/") {
		if strings.ContainsAny(a+b, "*") {
			return true
		}
	}
	return false
}
