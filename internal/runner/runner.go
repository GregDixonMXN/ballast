// Package runner is the execution half of dispatch: poll the control
// plane, run the adapter inside the assigned worktree, optionally run
// the work item's test command, and report the outcome. The server owns
// all state transitions; the runner only reports facts.
package runner

import (
	"ballast/internal/executil"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ballast/internal/agent"
	"ballast/internal/api"
)

// Client talks to the control-plane dispatch endpoints.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) do(method, path string, body, out any) (int, error) {
	var rdr *strings.Reader
	if body == nil {
		rdr = strings.NewReader("")
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := c.http().Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out); err != nil {
			return res.StatusCode, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	if res.StatusCode >= 400 {
		return res.StatusCode, fmt.Errorf("%s %s: %d", method, path, res.StatusCode)
	}
	return res.StatusCode, nil
}

// Register announces this node and returns its id + token.
func (c *Client) Register(info api.RunnerInfo) (id, token string, err error) {
	var out struct {
		Runner api.RunnerInfo `json:"runner"`
		Token  string         `json:"token"`
	}
	_, err = c.do("POST", "/runners", info, &out)
	return out.Runner.ID, out.Token, err
}

// Heartbeat marks liveness.
func (c *Client) Heartbeat(id string) error {
	_, err := c.do("POST", "/runners/"+id+"/heartbeat", map[string]string{}, nil)
	return err
}

// Poll returns the next work item; ok=false when the queue is empty
// (HTTP 204 is a valid empty answer, not an error).
func (c *Client) Poll(id string) (item api.WorkItem, ok bool, err error) {
	code, err := c.do("GET", "/runners/"+id+"/work", nil, &item)
	if code == http.StatusNoContent {
		return api.WorkItem{}, false, nil
	}
	if err != nil {
		return api.WorkItem{}, false, err
	}
	return item, true, nil
}

// SetStatus marks workspace progress (RUNNING while the agent works).
func (c *Client) SetStatus(wsID, status string) error {
	_, err := c.do("POST", "/workspaces/"+wsID+"/status",
		map[string]string{"status": status}, nil)
	return err
}

// Report posts the execution outcome; the server transitions state,
// builds the changeset on success, and returns its summary.
func (c *Client) Report(runnerID string, res api.WorkResult) (map[string]any, error) {
	var out map[string]any
	_, err := c.do("POST", "/runners/"+runnerID+"/results", res, &out)
	return out, err
}

// coordinationNote mirrors the server board shape.
type coordinationNote struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

// Claim announces a write scope for a workspace; returns overlapping
// holders as "pattern (owner)" lines. Empty means no overlap.
func (c *Client) Claim(wsID, pattern string) []string {
	var out struct {
		Overlaps []map[string]string `json:"overlaps"`
	}
	if _, err := c.do("POST", "/workspaces/"+wsID+"/claim", map[string]string{"pattern": pattern}, &out); err != nil {
		return nil
	}
	var lines []string
	for _, o := range out.Overlaps {
		lines = append(lines, o["pattern"]+" ("+o["owner"]+")")
	}
	return lines
}

// PostNote appends to the project blackboard as this workspace.
func (c *Client) PostNote(projectID, wsID, text string) error {
	var out map[string]any
	_, err := c.do("POST", "/projects/"+projectID+"/notes",
		map[string]string{"text": text, "workspace_id": wsID}, &out)
	return err
}

// Board reads the project blackboard newest-first, capped for prompts.
func (c *Client) Board(projectID string) []string {
	var list []coordinationNote
	if _, err := c.do("GET", "/projects/"+projectID+"/notes", nil, &list); err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for i, n := range list {
		if i >= 6 {
			break
		}
		owner := n.Author
		if len(owner) > 8 {
			owner = owner[:8]
		}
		out = append(out, "["+owner+"] "+oneLineNote(n.Text))
	}
	return out
}

func oneLineNote(s string) string {
	s = strings.ReplaceAll(s, "\n", " | ")
	if len(s) > 220 {
		return s[:220] + "..."
	}
	return s
}

// runTest executes an argv-style test command inside dir and captures it.
func runTest(ctx context.Context, dir, command string) (exit int, output string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return 0, ""
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = dir
	executil.Configure(cmd)
	home, err := os.MkdirTemp("", "ballast-test-home-*")
	if err != nil {
		return -1, err.Error()
	}
	defer os.RemoveAll(home)
	cmd.Env = append(cmd.Env, "HOME="+home)
	// Toolchain roots live in the operator's real home; a bare HOME
	// override orphans them (rustup: "no default toolchain", go: cold
	// module cache). Pass them through explicitly, resolved when unset.
	for _, kv := range toolchainEnv() {
		cmd.Env = append(cmd.Env, kv)
	}
	var buf executil.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), buf.String()
		}
		return -1, buf.String() + "\n" + err.Error()
	}
	return 0, buf.String()
}

// toolchainEnv passes language-toolchain roots into the scrubbed test
// environment, resolving defaults from the runner's real home when the
// operator hasn't set them explicitly.
func toolchainEnv() []string {
	var out []string
	add := func(key, def string) {
		if v := os.Getenv(key); v != "" {
			out = append(out, key+"="+v)
			return
		}
		if home, err := os.UserHomeDir(); err == nil && def != "" {
			out = append(out, key+"="+filepath.Join(home, def))
		}
	}
	add("RUSTUP_HOME", ".rustup")
	add("CARGO_HOME", ".cargo")
	add("GOPATH", "go")
	return out
}

// Executor runs one work item through an adapter and reports it.
// Client and Adapters are interfaces so tests fake both without HTTP
// or subprocesses.
type Executor struct {
	WorktreeRoot string
	Timeout      time.Duration
	Client       Reporter
	Adapters     []agent.Adapter
}

// Reporter is the control-plane surface the executor needs.
type Reporter interface {
	SetStatus(wsID, status string) error
	Report(runnerID string, res api.WorkResult) (map[string]any, error)
}

func pickAdapter(adapters []agent.Adapter, want string) agent.Adapter {
	for _, a := range adapters {
		if a.Name() == want {
			return a
		}
	}
	if want == "" {
		for _, a := range adapters {
			if a.Available(context.Background()) {
				return a
			}
		}
	}
	return nil
}

// RunOnce polls for work and, when present, executes it fully.
// Returns didWork=false when the queue was empty.
func (e *Executor) RunOnce(ctx context.Context, runnerID string, poll func() (api.WorkItem, bool, error)) (bool, error) {
	item, ok, err := poll()
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if e.WorktreeRoot != "" {
		root, err := filepath.EvalSymlinks(e.WorktreeRoot)
		if err != nil {
			return true, err
		}
		path, err := filepath.EvalSymlinks(item.Path)
		if err != nil {
			return true, err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true, fmt.Errorf("assigned path outside worktree root")
		}
	}
	a := pickAdapter(e.Adapters, item.Adapter)
	if a == nil {
		_, err := e.Client.Report(runnerID, api.WorkResult{WorkspaceID: item.WorkspaceID, ExitCode: -1, Stderr: "requested adapter unavailable"})
		return true, err
	}
	if err := e.Client.SetStatus(item.WorkspaceID, "RUNNING"); err != nil {
		return true, fmt.Errorf("mark running: %w", err)
	}
	// Context-aware adapters (the tool loop) learn their workspace and
	// project so coordination hooks can claim scopes and read the board.
	if ca, ok := a.(interface{ SetWork(wsID, projectID string) }); ok {
		ca.SetWork(item.WorkspaceID, item.ProjectID)
	}
	prompt := item.Prompt
	if a.Name() == "cmd" && item.Command != "" {
		prompt = item.Command // TASK_CMD runs raw; the description is not a shell script
	}
	ex, err := a.StartTask(ctx, item.TaskID, item.Path, prompt)
	res := api.WorkResult{WorkspaceID: item.WorkspaceID}
	if err != nil {
		res.ExitCode = -1
		res.Stderr = err.Error()
	} else {
		res.ExitCode = ex.ExitCode
		res.Stdout = ex.Stdout
		res.Stderr = ex.Stderr
	}
	if item.TestCommand != "" && res.ExitCode == 0 {
		res.TestRan = true
		res.TestExit, res.TestOutput = runTest(ctx, item.Path, item.TestCommand)
	}
	if _, err := e.Client.Report(runnerID, res); err != nil {
		return true, fmt.Errorf("report: %w", err)
	}
	return true, nil
}
