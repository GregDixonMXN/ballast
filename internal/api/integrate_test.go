package api

import (
	"os"
	"path/filepath"
	"testing"
)

// Approve → integrate moves the canonical head; the sibling built on the
// old base is triaged by the same call (CONFLICTED here — same line).
// Direct integrate of anything not APPROVED is refused. Runner tokens
// get 403: agents can never merge themselves.
func TestIntegrateFlow(t *testing.T) {
	repo := gitSeed(t)
	ts, tok, ids := testServer(t, repo)
	pid := ids["project"]

	mkTask := func(title string) string {
		code, out := call(t, ts, tok, "POST", "/projects/"+pid+"/tasks",
			map[string]string{"title": title})
		if code != 201 {
			t.Fatalf("task: %d %v", code, out)
		}
		return out["id"].(string)
	}
	tA, tB := mkTask("A"), mkTask("B")

	// Two workspaces → conflicting edits to backend.go.
	mkWS := func(tid string) (string, string) {
		code, out := call(t, ts, tok, "POST", "/projects/"+pid+"/workspaces",
			map[string]string{"task_id": tid})
		if code != 201 {
			t.Fatalf("workspace: %d %v", code, out)
		}
		return out["id"].(string), out["path"].(string)
	}
	wA, pA := mkWS(tA)
	wB, pB := mkWS(tB)
	os.WriteFile(filepath.Join(pA, "backend.go"), []byte("package api\n// A\n"), 0o644)
	os.WriteFile(filepath.Join(pB, "backend.go"), []byte("package api\n// B\n"), 0o644)

	mkCS := func(tid, ws string) string {
		code, out := call(t, ts, tok, "POST", "/workspaces/"+ws+"/changesets",
			map[string]string{"project_id": pid, "task_id": tid})
		if code != 201 {
			t.Fatalf("changeset: %d %v", code, out)
		}
		return out["id"].(string)
	}
	cA, cB := mkCS(tA, wA), mkCS(tB, wB)

	decide := func(id, d string) {
		code, _ := call(t, ts, tok, "POST", "/changesets/"+id+"/decision",
			map[string]string{"decision": d, "actor": "human"})
		if code != 200 {
			t.Fatalf("decide %s: %d", id, code)
		}
	}
	decide(cA, "approve")
	decide(cB, "approve")

	// Runner token must be rejected from merging.
	code, out := call(t, ts, tok, "POST", "/runners",
		map[string]any{"hostname": "r1"})
	runnerTok := out["token"].(string)
	if code, _ := call(t, ts, runnerTok, "POST", "/changesets/"+cA+"/integrate", nil); code != 403 {
		t.Fatalf("runner integrate = %d, want 403", code)
	}

	// Integrate A: merged, head moved, sibling B triaged in the response.
	code, out = call(t, ts, tok, "POST", "/changesets/"+cA+"/integrate", nil)
	if code != 200 || out["merged"] != true || out["status"] != "MERGED" {
		t.Fatalf("integrate A: %d %v", code, out)
	}

	// Sibling B changed the same line after the head moved → CONFLICTED
	// by A's sibling pass, so direct integrate is now refused with 409.
	code, out = call(t, ts, tok, "GET", "/changesets/"+cB, nil)
	if code != 200 || out["status"] != "CONFLICTED" {
		t.Fatalf("sibling B = %d %v, want CONFLICTED", code, out)
	}
	if code, _ := call(t, ts, tok, "POST", "/changesets/"+cB+"/integrate", nil); code != 409 {
		t.Fatalf("stale integrate = %d, want 409", code)
	}

	// Non-approved changesets are refused with 409.
	code, out = call(t, ts, tok, "POST", "/workspaces/"+wA+"/changesets",
		map[string]string{"project_id": pid, "task_id": tA})
	cC := out["id"].(string)
	if code, _ := call(t, ts, tok, "POST", "/changesets/"+cC+"/integrate", nil); code != 409 {
		t.Fatalf("unapproved integrate = %d, want 409", code)
	}
}
