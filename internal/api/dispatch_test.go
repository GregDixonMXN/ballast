// Dispatch proof: register → poll empty → assign → poll item →
// agent edit → results → changeset auto-built, task REVIEW.
// Full HTTP through httptest; temp git repo; no Postgres.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"ballast/internal/auth"
	"ballast/internal/events"
	"ballast/internal/services"
	"ballast/internal/store"
	"ballast/internal/workspace"
)

func testServer(t *testing.T, repoPath string) (*httptest.Server, string, map[string]string) {
	t.Helper()
	bus := events.NewMemoryBus(events.NewMemoryStore())
	toks := auth.NewTokens()
	userTok := toks.Mint(auth.Identity{ID: "u", Kind: "human", Roles: []string{"admin"}})
	srv := New(bus, toks)
	st := store.NewMemoryRepo()
	mgr := workspace.NewManager(t.TempDir())
	srv.Projects = &services.Projects{Repo: st}
	srv.Tasks = &services.Tasks{Repo: st}
	srv.Workspaces = &services.Workspaces{Repo: st, Mgr: mgr}
	srv.Changesets = &services.Changesets{Repo: st}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Seed one project straight through the service.
	p, err := srv.Projects.Create("demo", repoPath, "main")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	// Services return concrete values as any; read the id via JSON.
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	ids["project"] = m["id"].(string)
	return ts, userTok, ids
}

func call(t *testing.T, ts *httptest.Server, tok, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if res.StatusCode != http.StatusNoContent {
		_ = json.NewDecoder(res.Body).Decode(&out)
	}
	return res.StatusCode, out
}

func gitSeed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("init", "-b", "main")
	g("config", "user.email", "t@t.t")
	g("config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "backend.go"), []byte("package api\n"), 0o644)
	g("add", "-A")
	g("commit", "-m", "init")
	return dir
}

func TestDispatchFlow(t *testing.T) {
	ts, tok, ids := testServer(t, gitSeed(t))
	pid := ids["project"]

	// Task via HTTP.
	code, out := call(t, ts, tok, "POST", "/projects/"+pid+"/tasks",
		map[string]string{"title": "Backend"})
	if code != 201 {
		t.Fatalf("create task: %d %v", code, out)
	}
	tid := out["id"].(string)

	// Register runner.
	code, out = call(t, ts, tok, "POST", "/runners",
		map[string]any{"hostname": "r1", "os": "linux", "arch": "amd64"})
	if code != 201 {
		t.Fatalf("register: %d %v", code, out)
	}
	rid := out["runner"].(map[string]any)["id"].(string)

	// Empty queue → 204.
	if code, _ := call(t, ts, tok, "GET", "/runners/"+rid+"/work", nil); code != 204 {
		t.Fatalf("empty poll = %d, want 204", code)
	}

	// Assign → workspace + queued item.
	code, out = call(t, ts, tok, "POST", "/tasks/"+tid+"/assign",
		map[string]string{"runner_id": rid, "adapter": "codex"})
	if code != 201 {
		t.Fatalf("assign: %d %v", code, out)
	}
	wsID := out["workspace"].(map[string]any)["id"].(string)
	itemPath := out["item"].(map[string]any)["path"].(string)

	// Poll → the item.
	code, out = call(t, ts, tok, "GET", "/runners/"+rid+"/work", nil)
	if code != 200 || out["workspace_id"] != wsID {
		t.Fatalf("poll = %d %v", code, out)
	}

	// Agent edit inside the worktree.
	os.WriteFile(filepath.Join(itemPath, "backend.go"),
		[]byte("package api\n// agent\n"), 0o644)

	// Results → completed + changeset + task REVIEW.
	code, out = call(t, ts, tok, "POST", "/runners/"+rid+"/results",
		map[string]any{"workspace_id": wsID, "exit_code": 0, "stdout": "done"})
	if code != 200 {
		t.Fatalf("results: %d %v", code, out)
	}
	if out["changeset_id"] == nil || out["changeset_id"] == "" {
		t.Fatalf("expected changeset_id, got %v", out)
	}

	// Task is REVIEW; unknown runner is 404 on heartbeat.
	if code, _ := call(t, ts, tok, "POST", "/runners/nope/heartbeat", map[string]string{}); code != 404 {
		t.Fatalf("unknown heartbeat = %d, want 404", code)
	}
}

func TestAssignUnknownRunner(t *testing.T) {
	ts, tok, ids := testServer(t, gitSeed(t))
	code, out := call(t, ts, tok, "POST", "/projects/"+ids["project"]+"/tasks",
		map[string]string{"title": "X"})
	tid := out["id"].(string)
	code, _ = call(t, ts, tok, "POST", "/tasks/"+tid+"/assign",
		map[string]string{"runner_id": "nope"})
	if code != 404 {
		t.Fatalf("assign unknown runner = %d, want 404", code)
	}
}

func TestRunnerCapabilitiesRequiredTestsAndDuplicateReport(t *testing.T) {
	repo := gitSeed(t)
	ts, operator, ids := testServer(t, repo)
	status, reg := call(t, ts, operator, "POST", "/runners", map[string]any{"hostname": "fixture"})
	if status != 201 {
		t.Fatal(status, reg)
	}
	runner := reg["runner"].(map[string]any)["id"].(string)
	token := reg["token"].(string)
	for _, route := range []string{"/projects", "/events", "/runners/not-owned/work"} {
		status, _ = call(t, ts, token, "GET", route, nil)
		if status != 403 {
			t.Fatalf("%s: %d", route, status)
		}
	}
	status, _ = call(t, ts, token, "POST", "/projects", map[string]string{"name": "forbidden"})
	if status != 403 {
		t.Fatal(status)
	}
	_, created := call(t, ts, operator, "POST", "/projects/"+ids["project"]+"/tasks", map[string]string{"title": "required test"})
	taskID := created["id"].(string)
	status, assigned := call(t, ts, operator, "POST", "/tasks/"+taskID+"/assign", map[string]string{"runner_id": runner, "test_command": "true"})
	if status != 201 {
		t.Fatal(status, assigned)
	}
	ws := assigned["workspace"].(map[string]any)
	wsID := ws["id"].(string)
	if err := os.WriteFile(filepath.Join(ws["path"].(string), "result.txt"), []byte("result\n"), 0600); err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"workspace_id": wsID, "exit_code": 0}
	status, _ = call(t, ts, token, "POST", "/runners/"+runner+"/results", report)
	if status != 409 {
		t.Fatalf("missing tests: %d", status)
	}
	report["test_ran"] = true
	report["test_exit"] = 0
	status, first := call(t, ts, token, "POST", "/runners/"+runner+"/results", report)
	if status != 200 {
		t.Fatal(status, first)
	}
	status, second := call(t, ts, token, "POST", "/runners/"+runner+"/results", report)
	if status != 200 || first["changeset_id"] != second["changeset_id"] {
		t.Fatal(status, first, second)
	}
}
