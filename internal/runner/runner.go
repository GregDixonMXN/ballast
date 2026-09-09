// Package runner is the execution half of dispatch: poll the control
// plane, run the adapter inside the assigned worktree, optionally run
// the work item's test command, and report the outcome. The server owns
// all state transitions; the runner only reports facts.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
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
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
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

// runTest executes an argv-style test command inside dir and captures it.
func runTest(ctx context.Context, dir, command string) (exit int, output string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return 0, ""
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = dir
	var buf bytes.Buffer
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

// Executor runs one work item through an adapter and reports it.
// Client and Adapters are interfaces so tests fake both without HTTP
// or subprocesses.
type Executor struct {
	Client   Reporter
	Adapters []agent.Adapter
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
	if len(adapters) > 0 {
		return adapters[0]
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
	a := pickAdapter(e.Adapters, item.Adapter)
	if a == nil {
		return true, fmt.Errorf("no adapter available (wanted %q)", item.Adapter)
	}
	if err := e.Client.SetStatus(item.WorkspaceID, "RUNNING"); err != nil {
		return true, fmt.Errorf("mark running: %w", err)
	}
	ex, err := a.StartTask(ctx, item.TaskID, item.Path, item.Prompt)
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
