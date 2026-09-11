package overseer

import (
	"testing"
	"time"
)

func TestDuplicateTitles(t *testing.T) {
	o := New()
	tasks := []TaskView{
		{ID: "a1", Title: "Execution engine core (paper-only, no credentials)", Status: "TODO"},
		{ID: "b2", Title: "Execution engine core (paper-only, no credentials)", Status: "TODO"},
		{ID: "c3", Title: "Fee schedule crate (per-venue tables)", Status: "TODO"},
	}
	f := o.Sweep("p", tasks, nil)
	if len(f) != 1 {
		t.Fatalf("findings = %v", f)
	}
	// second sweep stays quiet (announced)
	if f := o.Sweep("p", tasks, nil); len(f) != 0 {
		t.Fatalf("repeat announced: %v", f)
	}
}

func TestFileCollision(t *testing.T) {
	o := New()
	ws := []WSView{
		{ID: "w1", TaskID: "t1", Status: "RUNNING", UpdatedAt: time.Now().UTC(), Files: []string{"Cargo.toml", "crates/a/lib.rs"}},
		{ID: "w2", TaskID: "t2", Status: "RUNNING", UpdatedAt: time.Now().UTC(), Files: []string{"Cargo.toml", "crates/b/lib.rs"}},
	}
	f := o.Sweep("p", nil, ws)
	found := false
	for _, s := range f {
		if len(s) >= 15 && s[:15] == "file collision:" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no collision flagged: %v", f)
	}
}

func TestStuck(t *testing.T) {
	o := New()
	ws := []WSView{
		{ID: "w1", TaskID: "t1", Status: "RUNNING", UpdatedAt: time.Now().UTC().Add(-time.Hour)},
	}
	f := o.Sweep("p", nil, ws)
	if len(f) != 1 {
		t.Fatalf("findings = %v", f)
	}
}

func TestClaimOverlap(t *testing.T) {
	o := New()
	f := o.CheckClaims(nil)
	if len(f) != 0 {
		t.Fatalf("empty = %v", f)
	}
}
