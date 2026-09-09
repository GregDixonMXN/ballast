package conflict

import "testing"

func TestSameFile(t *testing.T) {
	snaps := []FilesChanged{
		{WorkspaceID: "a", Files: []string{"backend.go"}},
		{WorkspaceID: "b", Files: []string{"frontend.tsx"}},
	}
	if got := DetectFiles("p", snaps); len(got) != 0 {
		t.Fatalf("expected no conflict, got %v", got)
	}
	snaps[1].Files = []string{"backend.go"}
	got := DetectFiles("p", snaps)
	if len(got) != 1 || got[0].Kind != SameFile {
		t.Fatalf("expected SAME_FILE, got %v", got)
	}
	if got[0].Status != Open || got[0].Severity != Warning {
		t.Fatalf("unexpected record %+v", got[0])
	}
}

func TestSameRegion(t *testing.T) {
	snaps := []FilesChanged{
		{WorkspaceID: "a", Hunks: map[string][][2]int{"x.go": {{10, 20}}}},
		{WorkspaceID: "b", Hunks: map[string][][2]int{"x.go": {{15, 18}}}},
	}
	if got := DetectRegions("p", snaps); len(got) == 0 {
		t.Fatal("expected SAME_REGION conflict")
	}
	disjoint := []FilesChanged{
		{WorkspaceID: "a", Hunks: map[string][][2]int{"x.go": {{10, 12}}}},
		{WorkspaceID: "b", Hunks: map[string][][2]int{"x.go": {{50, 55}}}},
	}
	if got := DetectRegions("p", disjoint); len(got) != 0 {
		t.Fatalf("expected no region conflict, got %v", got)
	}
}

func TestStale(t *testing.T) {
	if DetectStale("p", "w", "abc", "abc") != nil {
		t.Fatal("same base should not be stale")
	}
	c := DetectStale("p", "w", "abc0000000", "def0000000")
	if c == nil || c.Kind != StaleBase {
		t.Fatalf("expected STALE_BASE, got %+v", c)
	}
}
