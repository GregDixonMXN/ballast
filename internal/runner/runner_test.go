package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ballast/internal/agent"
	"ballast/internal/api"
)

type fakeAdapter struct {
	name    string
	exit    int
	stdout  string
	gotDir  string
	gotTask string
}

func (f *fakeAdapter) Name() string                   { return f.name }
func (f *fakeAdapter) Available(context.Context) bool { return true }
func (f *fakeAdapter) StartTask(_ context.Context, taskID, dir, _ string) (*agent.Execution, error) {
	f.gotDir, f.gotTask = dir, taskID
	return &agent.Execution{ExitCode: f.exit, Stdout: f.stdout,
		StartedAt: time.Now(), EndedAt: time.Now()}, nil
}
func (f *fakeAdapter) SendMessage(context.Context, string, string) error { return nil }
func (f *fakeAdapter) Stop(context.Context, string) error                { return nil }
func (f *fakeAdapter) Status(context.Context, string) (agent.Status, error) {
	return agent.Status{}, nil
}

type fakeReporter struct {
	statuses map[string]string
	reports  []api.WorkResult
}

func (f *fakeReporter) SetStatus(wsID, status string) error {
	if f.statuses == nil {
		f.statuses = map[string]string{}
	}
	f.statuses[wsID] = status
	return nil
}

func (f *fakeReporter) Report(_ string, res api.WorkResult) (map[string]any, error) {
	f.reports = append(f.reports, res)
	return map[string]any{}, nil
}

func TestRunOnceEmpty(t *testing.T) {
	e := &Executor{Client: &fakeReporter{}, Adapters: []agent.Adapter{&fakeAdapter{name: "x"}}}
	did, err := e.RunOnce(context.Background(), "r1", func() (api.WorkItem, bool, error) {
		return api.WorkItem{}, false, nil
	})
	if err != nil || did {
		t.Fatalf("empty queue: did=%v err=%v", did, err)
	}
}

func TestRunOnceExecutesAndReports(t *testing.T) {
	rep := &fakeReporter{}
	ad := &fakeAdapter{name: "codex", exit: 0, stdout: "done"}
	e := &Executor{Client: rep, Adapters: []agent.Adapter{ad}}
	item := api.WorkItem{WorkspaceID: "w1", TaskID: "t1", Path: "/tmp/w1", Prompt: "do it", Adapter: "codex"}
	did, err := e.RunOnce(context.Background(), "r1", func() (api.WorkItem, bool, error) {
		return item, true, nil
	})
	if err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	if rep.statuses["w1"] != "RUNNING" {
		t.Fatalf("statuses = %v", rep.statuses)
	}
	if ad.gotDir != "/tmp/w1" || ad.gotTask != "t1" {
		t.Fatalf("adapter got dir=%q task=%q", ad.gotDir, ad.gotTask)
	}
	if len(rep.reports) != 1 || rep.reports[0].ExitCode != 0 || rep.reports[0].Stdout != "done" {
		t.Fatalf("reports = %+v", rep.reports)
	}
	if rep.reports[0].TestRan {
		t.Fatal("no test command means no test run")
	}
}

func TestRunOnceFailure(t *testing.T) {
	rep := &fakeReporter{}
	ad := &fakeAdapter{name: "codex", exit: 3, stdout: "boom"}
	e := &Executor{Client: rep, Adapters: []agent.Adapter{ad}}
	item := api.WorkItem{WorkspaceID: "w9", TaskID: "t9", Path: "/tmp/w9", Adapter: "codex"}
	if _, err := e.RunOnce(context.Background(), "r1", func() (api.WorkItem, bool, error) {
		return item, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if rep.reports[0].ExitCode != 3 {
		t.Fatalf("reports = %+v", rep.reports)
	}
}

func TestRunTestCommand(t *testing.T) {
	exit, out := runTest(context.Background(), "/tmp", "git --version")
	if exit != 0 || out == "" {
		t.Fatalf("git --version: exit=%d out=%q", exit, out)
	}
	if exit, _ := runTest(context.Background(), "/tmp", ""); exit != 0 {
		t.Fatal("empty command must be a no-op success")
	}
}

func TestPollEmpty204(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	c := &Client{Base: ts.URL, Token: "x"}
	_, ok, err := c.Poll("r1")
	if err != nil || ok {
		t.Fatalf("204 must mean no work: ok=%v err=%v", ok, err)
	}
}
