package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"ballast/internal/changeset"
	"ballast/internal/events"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

func localFixture(t *testing.T) (*LocalRepo, string, project.Project) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private", "state.json")
	l, err := OpenLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	p := project.NewProject("", "fixture", "/tmp/synthetic-repo", "main")
	if err = l.SaveProject(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return l, path, p
}
func TestLocalRoundTripAndOwnedValues(t *testing.T) {
	l, path, p := localFixture(t)
	ctx := context.Background()
	ta := task.New(p.ID, "fixture task", "", []string{"src/*"})
	if err := l.SaveTask(ctx, ta); err != nil {
		t.Fatal(err)
	}
	ta.Scopes[0] = "caller edit"
	got, err := l.GetTask(ctx, ta.ID)
	if err != nil || got.Scopes[0] != "src/*" {
		t.Fatalf("input alias: %+v %v", got, err)
	}
	got.Scopes[0] = "reader edit"
	got, _ = l.GetTask(ctx, ta.ID)
	if got.Scopes[0] != "src/*" {
		t.Fatal("reader changed stored slice")
	}
	w := workspace.Workspace{ID: "workspace", ProjectID: p.ID, TaskID: ta.ID, Status: workspace.Ready}
	if err = l.SaveWorkspace(ctx, w); err != nil {
		t.Fatal(err)
	}
	c := changeset.Changeset{ID: "changeset", ProjectID: p.ID, TaskID: ta.ID, Status: changeset.InReview, Files: []string{"a.go"}}
	if err = l.SaveChangeset(ctx, c); err != nil {
		t.Fatal(err)
	}
	e := events.New(p.ID, events.ActorSystem, "", events.TaskCreated, ta.ID, map[string]any{"nested": map[string]any{"value": "initial"}})
	if err = l.Append(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Metadata["nested"].(map[string]any)["value"] = "caller edit"
	ev, _ := l.List(ctx, p.ID, 10)
	ev[0].Metadata["nested"].(map[string]any)["value"] = "reader edit"
	if err = l.SetCanonicalHead(ctx, p.ID, "new-head"); err != nil {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	gotP, err := reopened.GetProject(ctx, p.ID)
	if err != nil || gotP.CanonicalSHA != "new-head" {
		t.Fatal(gotP, err)
	}
	if ws, _ := reopened.ListWorkspaces(ctx, p.ID); len(ws) != 1 {
		t.Fatal(ws)
	}
	if cs, _ := reopened.ListChangesets(ctx, p.ID); len(cs) != 1 {
		t.Fatal(cs)
	}
	ev, err = reopened.List(ctx, p.ID, 10)
	if err != nil || len(ev) != 1 || ev[0].Metadata["nested"].(map[string]any)["value"] != "initial" {
		t.Fatal(ev, err)
	}
}
func TestLocalFailedPublicationDoesNotLeak(t *testing.T) {
	l, path, p := localFixture(t)
	ctx := context.Background()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the destination makes atomic rename fail, without relying
	// on permissions or touching any state outside this test's temporary root.
	if err = os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	phantom := project.NewProject("", "must-not-save", "/tmp/synthetic", "main")
	if err = l.SaveProject(ctx, phantom); err == nil {
		t.Fatal("expected rename failure")
	}
	if _, err = l.GetProject(ctx, phantom.ID); err == nil {
		t.Fatal("failed mutation is readable")
	}
	if err = l.SetCanonicalHead(ctx, p.ID, "must-not-save"); err == nil {
		t.Fatal("expected update failure")
	}
	got, _ := l.GetProject(ctx, p.ID)
	if got.CanonicalSHA != "" {
		t.Fatal("failed update changed memory")
	}
	if err = l.Append(ctx, events.New(p.ID, events.ActorSystem, "", events.TestPassed, "", nil)); err == nil {
		t.Fatal("expected append failure")
	}
	if ev, _ := l.List(ctx, p.ID, 10); len(ev) != 0 {
		t.Fatal("failed append is readable")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(path)
	if string(current) != string(before) {
		t.Fatal("durable snapshot changed on failure")
	}
	ta := task.New(p.ID, "success after failure", "", nil)
	if err = l.SaveTask(ctx, ta); err != nil {
		t.Fatal(err)
	}
	l.Close()
	reopened, err := OpenLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err = reopened.GetProject(ctx, phantom.ID); err == nil {
		t.Fatal("later success persisted failed earlier mutation")
	}
	if ev, _ := reopened.List(ctx, p.ID, 10); len(ev) != 0 {
		t.Fatal("later success persisted failed event")
	}
}
func TestLocalPostPublicationFailureStopsStore(t *testing.T) {
	l, path, p := localFixture(t)
	ctx := context.Background()
	l.syncDir = func(string) error { return errors.New("synthetic directory sync failure") }
	if err := l.SetCanonicalHead(ctx, p.ID, "published"); err == nil {
		t.Fatal("expected durability error")
	}
	if _, err := l.GetProject(ctx, p.ID); err == nil {
		t.Fatal("uncertain store should fail reads")
	}
	if err := l.SetCanonicalHead(ctx, p.ID, "later"); err == nil {
		t.Fatal("uncertain store should reject writes")
	}
	l.Close()
	reopened, err := OpenLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, _ := reopened.GetProject(ctx, p.ID)
	if got.CanonicalSHA != "published" {
		t.Fatal("reopen did not read published snapshot")
	}
}
func TestLocalLockLifetimeAndPrivateFiles(t *testing.T) {
	l, path, p := localFixture(t)
	if other, err := OpenLocal(path); err == nil {
		other.Close()
		t.Fatal("second holder admitted")
	}
	for _, name := range []string{path, path + ".lock"} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	info, _ := os.Stat(filepath.Dir(path))
	if info.Mode().Perm() != 0700 {
		t.Fatal(info.Mode())
	}
	l.Close()
	if err := l.SetCanonicalHead(context.Background(), p.ID, "after-close"); err == nil {
		t.Fatal("closed writer accepted")
	}
	other, err := OpenLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
}
func TestLocalRejectsUnsafeOrCorruptFiles(t *testing.T) {
	for _, name := range []string{"public-state", "public-lock", "symlink-state", "symlink-lock", "invalid-json", "incomplete", "wrong-version", "wrong-identity", "orphan-task"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			s := emptySnapshot()
			switch name {
			case "invalid-json":
				os.WriteFile(path, []byte("{"), 0600)
			case "incomplete":
				os.WriteFile(path, []byte(`{"version":1}`), 0600)
			case "wrong-version":
				s.Version = 2
			case "wrong-identity":
				s.Projects["wrong"] = project.Project{ID: "other"}
			case "orphan-task":
				s.Tasks["task"] = task.Task{ID: "task", ProjectID: "missing"}
			}
			if name != "invalid-json" && name != "incomplete" {
				b, _ := json.Marshal(s)
				os.WriteFile(path, b, 0600)
			}
			switch name {
			case "public-state":
				os.Chmod(path, 0644)
			case "public-lock":
				os.WriteFile(path+".lock", nil, 0644)
			case "symlink-state":
				os.Rename(path, path+".target")
				os.Symlink(path+".target", path)
			case "symlink-lock":
				os.WriteFile(path+".target", nil, 0600)
				os.Symlink(path+".target", path+".lock")
			}
			if l, err := OpenLocal(path); err == nil {
				l.Close()
				t.Fatal("unsafe/corrupt state accepted")
			}
		})
	}
}
func TestLocalLockReplacementFailsClosed(t *testing.T) {
	l, path, p := localFixture(t)
	if err := os.Rename(path+".lock", path+".old-lock"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.GetProject(context.Background(), p.ID); err == nil {
		t.Fatal("replaced lifetime lock went unnoticed")
	}
}
func TestLocalConcurrentSnapshots(t *testing.T) {
	l, path, p := localFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ta := task.New(p.ID, fmt.Sprintf("task %d", i), "", nil)
			if err := l.SaveTask(ctx, ta); err != nil {
				t.Error(err)
			}
			if _, err := l.ListTasks(ctx, p.ID); err != nil {
				t.Error(err)
			}
			if err := l.Append(ctx, events.New(p.ID, events.ActorSystem, "", events.TaskCreated, ta.ID, nil)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	l.Close()
	other, err := OpenLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tasks, err := other.ListTasks(ctx, p.ID)
	if err != nil || len(tasks) != 12 {
		t.Fatal(len(tasks), err)
	}
	ev, err := other.List(ctx, p.ID, 30)
	if err != nil || len(ev) != 12 {
		t.Fatal(len(ev), err)
	}
}
func TestLocalCanceledWriteAndSerializationFailure(t *testing.T) {
	l, _, p := localFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.SetCanonicalHead(ctx, p.ID, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := l.Append(context.Background(), events.New(p.ID, events.ActorSystem, "", events.TestPassed, "", map[string]any{"invalid": func() {}})); err == nil {
		t.Fatal("unsupported metadata accepted")
	}
	if ev, _ := l.List(context.Background(), p.ID, 10); len(ev) != 0 {
		t.Fatal(ev)
	}
}

func TestLocalRejectsFIFOWithoutWaiting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if l, err := OpenLocal(path); err == nil {
		l.Close()
		t.Fatal("FIFO state accepted")
	}
}
