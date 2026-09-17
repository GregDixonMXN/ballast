package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Drift pair: A edits line 3, B edits line 5 of the same file. Adjacent
// hunks share context, so B no longer applies after A merges — but the two
// intents are independent. The stub verdict decides the triage.
func jevStub(t *testing.T, choice string, confidence float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{
				"route": map[string]any{"choice": choice, "confidence": confidence},
			},
		})
	}))
}

func driftScenario(t *testing.T) (ts *httptest.Server, tok string, cA, cB string) {
	t.Helper()
	repo := gitSeed(t)
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	base := "package api\n// l2\n// l3\n// l4\n// l5\n// l6\n// l7\n// l8\n"
	os.WriteFile(filepath.Join(repo, "backend.go"), []byte(base), 0o644)
	g("add", "-A")
	g("commit", "-m", "base")

	var ids map[string]string
	ts, tok, ids = testServer(t, repo)
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
	writeLine := func(path string, line int, replacement string) {
		b, _ := os.ReadFile(path)
		s := string(b)
		n := 0
		out := ""
		for _, l := range splitLines(s) {
			n++
			if n == line {
				out += replacement
			} else {
				out += l
			}
		}
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeLine(filepath.Join(pA, "backend.go"), 3, "// A\n")
	writeLine(filepath.Join(pB, "backend.go"), 5, "// B\n")

	mkCS := func(tid, ws string) string {
		code, out := call(t, ts, tok, "POST", "/workspaces/"+ws+"/changesets",
			map[string]string{"project_id": pid, "task_id": tid})
		if code != 201 {
			t.Fatalf("changeset: %d %v", code, out)
		}
		return out["id"].(string)
	}
	cA, cB = mkCS(tA, wA), mkCS(tB, wB)

	decide := func(id string) {
		code, _ := call(t, ts, tok, "POST", "/changesets/"+id+"/decision",
			map[string]string{"decision": "approve", "actor": "human"})
		if code != 200 {
			t.Fatalf("decide %s", id)
		}
	}
	decide(cA)
	decide(cB)
	return ts, tok, cA, cB
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		cur += string(r)
		if r == '\n' {
			out = append(out, cur)
			cur = ""
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func TestJevTriageRebasesDrift(t *testing.T) {
	srv := jevStub(t, "rebasable", 0.8)
	defer srv.Close()
	t.Setenv("BALLAST_JEV", "1")
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)

	ts, tok, cA, cB := driftScenario(t)
	code, out := call(t, ts, tok, "POST", "/changesets/"+cA+"/integrate", nil)
	if code != 200 || out["merged"] != true {
		t.Fatalf("integrate A: %d %v", code, out)
	}
	rev, _ := out["revalidated"].(map[string]any)
	if rev["rebase"] != float64(1) {
		t.Fatalf("revalidated = %v, want one rebase", out["revalidated"])
	}
	code, out = call(t, ts, tok, "GET", "/changesets/"+cB, nil)
	if code != 200 || out["status"] != "NEEDS_REBASE" {
		t.Fatalf("sibling B = %d %v, want NEEDS_REBASE", code, out)
	}
}

func TestJevTriageConflictsOnClash(t *testing.T) {
	srv := jevStub(t, "conflicted", 0.9)
	defer srv.Close()
	t.Setenv("BALLAST_JEV", "1")
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)

	ts, tok, cA, cB := driftScenario(t)
	if code, _ := call(t, ts, tok, "POST", "/changesets/"+cA+"/integrate", nil); code != 200 {
		t.Fatalf("integrate A: %d", code)
	}
	code, out := call(t, ts, tok, "GET", "/changesets/"+cB, nil)
	if code != 200 || out["status"] != "CONFLICTED" {
		t.Fatalf("sibling B = %d %v, want CONFLICTED", code, out)
	}
}

func TestJevTriageTornJudgmentStaysConflicted(t *testing.T) {
	srv := jevStub(t, "rebasable", 0.3) // right answer, no conviction
	defer srv.Close()
	t.Setenv("BALLAST_JEV", "1")
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)

	ts, tok, cA, cB := driftScenario(t)
	if code, _ := call(t, ts, tok, "POST", "/changesets/"+cA+"/integrate", nil); code != 200 {
		t.Fatalf("integrate A: %d", code)
	}
	code, out := call(t, ts, tok, "GET", "/changesets/"+cB, nil)
	if code != 200 || out["status"] != "CONFLICTED" {
		t.Fatalf("sibling B = %d %v, want CONFLICTED", code, out)
	}
}

func TestJevTriageFlagOffStaysConflicted(t *testing.T) {
	t.Setenv("BALLAST_JEV", "")
	t.Setenv("JEV_API_KEY", "test-key")

	ts, tok, cA, cB := driftScenario(t)
	if code, _ := call(t, ts, tok, "POST", "/changesets/"+cA+"/integrate", nil); code != 200 {
		t.Fatalf("integrate A: %d", code)
	}
	code, out := call(t, ts, tok, "GET", "/changesets/"+cB, nil)
	if code != 200 || out["status"] != "CONFLICTED" {
		t.Fatalf("sibling B = %d %v, want CONFLICTED (flag off)", code, out)
	}
}
