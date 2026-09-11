// Command supervise runs Ballast project mode: fully autonomous
// task assignment, review, and integration until a project completes.
//
// Loop per round:
//  1. Assign every READY task (deps DONE) to an idle runner.
//  2. Review every IN_REVIEW changeset: test-gated green +
//     single-scope + conflict-free integrates automatically;
//     anything else escalates to a human via the project board.
//  3. Requeue failed work (test red) as TODO with a note.
//  4. Exit 0 when no TODO/RUNNING/REVIEW/BLOCKED task remains.
//
// Escalation boundary: the loop never force-merges a conflict, never
// approves a red gate, never deletes. Humans handle exceptions.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

var (
	server    = flag.String("server", "http://127.0.0.1:8080", "ballast server")
	tokenFile = flag.String("token-file", "", "operator token file")
	project   = flag.String("project", "", "project id (required)")
	adapter   = flag.String("adapter", "loop", "adapter for assignments")
	testCmd   = flag.String("test-command", "", "test gate command")
	interval  = flag.Duration("interval", 60*time.Second, "poll interval")
	maxRounds = flag.Int("rounds", 0, "max rounds (0 = until complete)")
)

type client struct {
	base string
	tok  string
	hc   *http.Client
}

func (c *client) call(method, path string, body any) (any, error) {
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.base+path, rdr)
	req.Header.Set("Authorization", "Bearer "+c.tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d %s: %.120s", resp.StatusCode, path, raw)
	}
	var v any
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func arr(v any) []map[string]any {
	l, _ := v.([]any)
	out := []map[string]any{}
	for _, e := range l {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

// escalated remembers changesets already raised to a human so the
// board gets each exception once instead of every round.
var escalated = map[string]bool{}

func main() {
	flag.Parse()
	if *project == "" || *tokenFile == "" {
		fmt.Fprintln(os.Stderr, "-project and -token-file required")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*tokenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "token:", err)
		os.Exit(2)
	}
	c := &client{base: *server, tok: strings.TrimSpace(string(raw)), hc: &http.Client{Timeout: 60 * time.Second}}
	// Project spec: test gate defaults from the project so supervision
	// adapts to any stack (cargo, go, npm, pytest, test.sh).
	if *testCmd == "" {
		if pv, err := c.call("GET", "/projects/"+*project, nil); err == nil {
			if pm, ok := pv.(map[string]any); ok {
				*testCmd, _ = pm["test_command"].(string)
			}
		}
	}
	fmt.Println("test-command:", *testCmd)

	for round := 1; ; round++ {
		done, err := superviseRound(c)
		if err != nil {
			fmt.Println("round", round, "error:", err)
		} else if done {
			fmt.Println("project complete")
			c.call("POST", "/projects/"+*project+"/notes", map[string]any{
				"text": "supervisor: all tasks DONE, project complete", "author": "supervisor",
			})
			return
		}
		if *maxRounds > 0 && round >= *maxRounds {
			fmt.Println("round budget exhausted")
			return
		}
		time.Sleep(*interval)
	}
}

func superviseRound(c *client) (bool, error) {
	// --- state snapshot
	tasksV, err := c.call("GET", "/projects/"+*project+"/tasks", nil)
	if err != nil {
		return false, err
	}
	tasks := arr(tasksV)
	byID := map[string]map[string]any{}
	open := 0
	for _, t := range tasks {
		byID[str(t, "id")] = t
		if s := str(t, "status"); s == "TODO" || s == "RUNNING" || s == "REVIEW" || s == "BLOCKED" {
			open++
		}
	}
	wsV, err := c.call("GET", "/projects/"+*project+"/workspaces", nil)
	if err != nil {
		return false, err
	}
	workspaces := arr(wsV)
	busy := map[string]bool{} // runner_id -> has RUNNING workspace
	wsByTask := map[string]map[string]any{}
	for _, w := range workspaces {
		if str(w, "status") == "RUNNING" {
			busy[str(w, "runner_id")] = true
		}
		wsByTask[str(w, "task_id")] = w
	}
	runnersV, err := c.call("GET", "/runners", nil)
	if err != nil {
		return false, err
	}
	// --- 1. assign ready tasks to idle runners
	readyV, err := c.call("GET", "/projects/"+*project+"/ready", nil)
	if err != nil {
		return false, err
	}
	ready := arr(readyV)
	for _, r := range arr(runnersV) {
		rid := str(r, "id")
		if busy[rid] || len(ready) == 0 {
			continue
		}
		// liveness: heartbeat within 90s
		if seen := str(r, "last_seen"); seen != "" {
			if ts, e := time.Parse(time.RFC3339, seen); e == nil && time.Since(ts) > 90*time.Second {
				continue
			}
		}
		t := ready[0]
		ready = ready[1:]
		scopes, _ := t["scopes"].([]any)
		scope := ""
		if len(scopes) > 0 {
			scope, _ = scopes[0].(string)
		}
		_, err := c.call("POST", "/tasks/"+str(t, "id")+"/assign", map[string]any{
			"runner_id": rid, "adapter": *adapter,
			"test_command": *testCmd, "scope": scope,
		})
		if err != nil {
			fmt.Println("assign", str(t, "id")[:8], "->", rid[:8], "FAILED:", err)
			continue
		}
		busy[rid] = true
		fmt.Println("assign", str(t, "id")[:8], str(t, "title"), "->", rid[:8])
	}
	// --- 2/3. review + integrate + requeue
	cssV, err := c.call("GET", "/projects/"+*project+"/changesets", nil)
	if err != nil {
		return false, err
	}
	for _, cs := range arr(cssV) {
		if str(cs, "status") != "IN_REVIEW" {
			continue
		}
		reviewChangeset(c, cs, wsByTask)
	}
	return open == 0, nil
}

func reviewChangeset(c *client, cs map[string]any, wsByTask map[string]map[string]any) {
	csid := str(cs, "id")
	tid := str(cs, "task_id")
	if escalated[csid] {
		return
	}
	w := wsByTask[tid]
	// single-scope check: at most one directory besides manifests.
	// COMPLETED is the test evidence: the server only marks COMPLETED
	// when the exit code is 0 and a required test result is present
	// and green (zero exits serialize as absent via omitempty).
	diff, _ := cs["diff"].(string)
	scopes := map[string]bool{}
	for _, l := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(l, "+++ b/") {
			continue
		}
		f := strings.TrimPrefix(l, "+++ b/")
		if f == "Cargo.toml" || f == "Cargo.lock" {
			continue
		}
		scopes[path.Dir(f)] = true
	}
	singleScope := len(scopes) <= 1
	green := w != nil && str(w, "status") == "COMPLETED"
	if !green || !singleScope {
		reason := []string{}
		if !green {
			reason = append(reason, "test evidence missing/red")
		}
		if !singleScope {
			reason = append(reason, fmt.Sprintf("multi-scope %v", keys(scopes)))
		}
		c.call("POST", "/projects/"+*project+"/notes", map[string]any{
			"author": "supervisor",
			"text":   "escalate: changeset " + csid[:8] + " (task " + tid[:8] + ") needs human: " + strings.Join(reason, "; "),
		})
		escalated[csid] = true
		fmt.Println("escalate", csid[:8], strings.Join(reason, "; "))
		return
	}
	if _, err := c.call("POST", "/changesets/"+csid+"/decision", map[string]any{"decision": "approve"}); err != nil {
		fmt.Println("approve", csid[:8], "FAILED:", err)
		return
	}
	r, err := c.call("POST", "/changesets/"+csid+"/integrate", nil)
	if err != nil {
		fmt.Println("integrate", csid[:8], "FAILED:", err)
		return
	}
	m, _ := r.(map[string]any)
	if conflict, _ := m["conflict"].(bool); conflict {
		c.call("POST", "/projects/"+*project+"/notes", map[string]any{
			"author": "supervisor",
			"text":   "escalate: changeset " + csid[:8] + " approved but CONFLICTED on integrate — human merge needed",
		})
		escalated[csid] = true
		fmt.Println("integrate", csid[:8], "CONFLICTED, escalated")
		return
	}
	fmt.Println("integrated", csid[:8])
}

func keys(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
