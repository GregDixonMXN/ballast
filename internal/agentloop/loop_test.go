package agentloop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubModel replays scripted replies for loop tests.
func stubModel(t *testing.T, replies []chatMessage) *httptest.Server {
	t.Helper()
	var n int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n >= len(replies) {
			t.Fatalf("model called %d times, only %d scripted", n+1, len(replies))
		}
		msg := replies[n]
		n++
		_ = json.NewEncoder(w).Encode(chatResponse{Choices: []struct {
			Message chatMessage `json:"message"`
			Finish  string      `json:"finish_reason"`
		}{{Message: msg, Finish: "stop"}}})
	}))
}

func toolReply(id, name, args string) chatMessage {
	return chatMessage{Role: "assistant", ToolCalls: []toolCall{{
		ID: id, Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: name, Arguments: args},
	}}}
}

func TestLoopWritesFileAndFinishes(t *testing.T) {
	srv := stubModel(t, []chatMessage{
		toolReply("c1", "write_file", `{"path":"hello.txt","content":"hi"}`),
		{Role: "assistant", Content: "done, wrote hello.txt"},
	})
	defer srv.Close()
	wd := t.TempDir()
	ad := NewLoop("loop", LoopConfig{Model: Config{BaseURL: srv.URL, APIKey: "k", Model: "m"}})
	ex, err := ad.StartTask(context.Background(), "t1", wd, "write hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if ex.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", ex.ExitCode, ex.Stderr)
	}
	raw, err := os.ReadFile(filepath.Join(wd, "hello.txt"))
	if err != nil || string(raw) != "hi" {
		t.Fatalf("file = %q, %v", raw, err)
	}
	if !strings.Contains(ex.Stdout, "write_file") {
		t.Fatalf("transcript missing tool turn:\n%s", ex.Stdout)
	}
}

func TestLoopReportsToolErrorAndContinues(t *testing.T) {
	srv := stubModel(t, []chatMessage{
		toolReply("c1", "read_file", `{"path":"/etc/shadow-nope"}`),
		{Role: "assistant", Content: "could not read, stopping"},
	})
	defer srv.Close()
	ad := NewLoop("loop", LoopConfig{Model: Config{BaseURL: srv.URL, APIKey: "k", Model: "m"}})
	ex, err := ad.StartTask(context.Background(), "t1", t.TempDir(), "read a file")
	if err != nil {
		t.Fatal(err)
	}
	if ex.ExitCode != 0 {
		t.Fatalf("exit=%d", ex.ExitCode)
	}
	if !strings.Contains(ex.Stdout, "ERROR") {
		t.Fatalf("expected error turn in transcript:\n%s", ex.Stdout)
	}
}

func TestPathEscapeRefused(t *testing.T) {
	r := NewRegistry(nil)
	_, err := r.Execute(context.Background(), t.TempDir(), "read_file", map[string]any{"path": "../../x"})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected escape refusal, got %v", err)
	}
}

func TestTurnBudgetTrips(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(chatResponse{Choices: []struct {
			Message chatMessage `json:"message"`
			Finish  string      `json:"finish_reason"`
		}{{Message: toolReply("c", "list_dir", `{}`), Finish: "stop"}}})
	}))
	defer srv.Close()
	ad := NewLoop("loop", LoopConfig{Model: Config{BaseURL: srv.URL, APIKey: "k", Model: "m"}, MaxTurns: 3})
	ex, err := ad.StartTask(context.Background(), "t1", t.TempDir(), "loop forever")
	if err != nil {
		t.Fatal(err)
	}
	if ex.ExitCode != 2 || calls != 3 {
		t.Fatalf("exit=%d calls=%d stderr=%s", ex.ExitCode, calls, ex.Stderr)
	}
}
