package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"ballast/internal/agent"

	"github.com/google/uuid"
)

// LoopConfig bounds the run. Zero MaxTurns defaults to 60.
type LoopConfig struct {
	Model    Config
	Gate     Gate
	MaxTurns int
	// OnTurn receives a one-line human summary after every model/tool
	// turn. May be nil. Used for progress visibility while the run lives.
	OnTurn func(summary string)
	// Coordination hooks (all optional). Board seeds the run with the
	// project blackboard; Note posts to it; Claim announces a write
	// scope and returns overlapping holders as "pattern (owner)" lines.
	Board func() []string
	Note  func(text string) error
	Claim func(pattern string) []string
}

// LoopAdapter implements agent.Adapter by driving the model directly:
// prompt -> model -> tool calls -> execute in worktree -> results ->
// model ... until the model answers with no tool calls (done) or a
// bound trips. The full transcript lands in Execution.Stdout, so a
// failed run always explains itself.
type LoopAdapter struct {
	name string
	cfg  LoopConfig
	mu   sync.Mutex
	// work scope, set via SetWork when the runner provides it.
	wsID      string
	projectID string
}

func NewLoop(name string, cfg LoopConfig) *LoopAdapter {
	if name == "" {
		name = "loop"
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 60
	}
	return &LoopAdapter{name: name, cfg: cfg}
}

func (l *LoopAdapter) Name() string { return l.name }

func (l *LoopAdapter) Available(_ context.Context) bool { return true }

// SetWork binds the run to its workspace/project for coordination
// (claims, notes). Called by the executor when supported; the loop
// works without it.
func (l *LoopAdapter) SetWork(wsID, projectID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wsID = wsID
	l.projectID = projectID
}

// Scope returns the bound workspace/project, or empty strings.
func (l *LoopAdapter) Scope() (wsID, projectID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.wsID, l.projectID
}

// SetHooks attaches coordination callbacks after construction (the
// runner builds the adapter before its API client exists).
func (l *LoopAdapter) SetHooks(board func() []string, note func(string) error, claim func(string) []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cfg.Board = board
	l.cfg.Note = note
	l.cfg.Claim = claim
}

func (l *LoopAdapter) SendMessage(_ context.Context, _, _ string) error {
	return fmt.Errorf("streaming input not supported; start a new task turn")
}

func (l *LoopAdapter) Stop(_ context.Context, _ string) error {
	return fmt.Errorf("in-process loop stops via context cancel")
}

func (l *LoopAdapter) Status(_ context.Context, _ string) (agent.Status, error) {
	return agent.Status{}, fmt.Errorf("in-process loop has no live handle")
}

const systemPrompt = `You are a senior software engineer working inside a repository checkout. The task is given as the first user message.

Work in two phases and do not linger in phase 1:
1. EXPLORE (at most ~8 tool calls): list_dir/read_file on the WORKSPACE files only — Cargo.toml, README, ARCHITECTURE, the crates named in the task. Never read dependency sources under ~/.cargo or registry paths; use run_shell sparingly (prefer targeted reads over greps of giant files).
2. BUILD: write the code, then run the task's acceptance commands with run_shell and fix failures. Keep every new file small and focused.

Rules:
- Make the smallest change that satisfies the task's acceptance criteria.
- Money/price/quantity code must never use binary floating point.
- Never print secrets or API keys. Never exfiltrate anything off-machine.
- When the work is complete AND acceptance commands pass, reply with a short summary and no tool calls. An empty reply is scored as failure — always summarize.`

// StartTask runs the loop to completion and returns the transcript.
// Exit codes follow the platform convention: 0 done, 2 blocked by the
// gate or a bound, 1 internal error.
func (l *LoopAdapter) StartTask(ctx context.Context, taskID, workspace, prompt string) (*agent.Execution, error) {
	started := time.Now().UTC()
	id := uuid.NewString()
	var log strings.Builder
	var logMu sync.Mutex
	turn := func(format string, args ...any) {
		s := fmt.Sprintf(format, args...)
		logMu.Lock()
		log.WriteString(s + "\n")
		logMu.Unlock()
		if l.cfg.OnTurn != nil {
			l.cfg.OnTurn(s)
		}
	}

	client := NewClient(l.cfg.Model)
	reg := NewRegistry(l.cfg.Gate)
	reg.OnNote = l.cfg.Note
	reg.OnClaim = l.cfg.Claim
	userContent := prompt
	if l.cfg.Board != nil {
		if lines := l.cfg.Board(); len(lines) > 0 {
			if len(lines) > 6 {
				lines = lines[:6]
			}
			userContent = "PROJECT BOARD (what siblings learned before you):\n- " +
				strings.Join(lines, "\n- ") + "\n\n" + prompt
		}
	}
	msgs := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}
	tools := reg.ChatTools()
	turn("turn 0: task received (%d chars), %d tools available", len(prompt), len(tools))

	for t := 1; t <= l.cfg.MaxTurns; t++ {
		if err := ctx.Err(); err != nil {
			return done(id, taskID, workspace, started, log.String(), "", 2, "cancelled: %v", err), nil
		}
		// One slow API response must not kill a 20-turn run: retry
		// transient transport failures, fail only on persistent errors.
		var reply Reply
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			reply, err = client.Complete(ctx, msgs, tools)
			if err == nil {
				break
			}
			if !isTransient(err) || attempt == 3 {
				break
			}
			turn("turn %d: model call attempt %d failed (%v), retrying", t, attempt, err)
			select {
			case <-ctx.Done():
				return done(id, taskID, workspace, started, log.String(), "", 2, "cancelled: %v", ctx.Err()), nil
			case <-time.After(time.Duration(attempt*5) * time.Second):
			}
		}
		if err != nil {
			return done(id, taskID, workspace, started, log.String(), "", 1, "model call failed on turn %d: %v", t, err), nil
		}
		msg := reply.Message
		msgs = append(msgs, msg)
		// Reasoning models can burn the output budget thinking and come
		// back length-cut with no content and no calls. Nudge to continue
		// instead of scoring an empty finish.
		if len(msg.ToolCalls) == 0 && strings.TrimSpace(msg.Content) == "" && reply.Finish == "length" {
			turn("turn %d: output cut by length limit, asking model to continue", t)
			msgs = append(msgs, chatMessage{Role: "user", Content: "Your reply was cut off. Continue exactly where you left off."})
			continue
		}
		if len(msg.ToolCalls) == 0 {
			text := strings.TrimSpace(msg.Content)
			if text == "" {
				// Fail closed: an empty finish proves nothing and must
				// never pass the test gate on an unverified tree.
				return done(id, taskID, workspace, started, log.String(), "", 2, "model finished with empty summary; work unverifiable"), nil
			}
			turn("turn %d: model finished: %s", t, truncate(text, 500))
			return done(id, taskID, workspace, started, log.String(), text, 0, ""), nil
		}
		turn("turn %d: %d tool call(s)", t, len(msg.ToolCalls))
		results := l.executeTurn(ctx, reg, workspace, msg.ToolCalls, turn)
		for _, r := range results {
			msgs = append(msgs, chatMessage{
				Role:       "tool",
				ToolCallID: r.id,
				Content:    r.modelResult,
			})
		}
	}
	return done(id, taskID, workspace, started, log.String(), "", 2, "turn budget exhausted (%d turns)", l.cfg.MaxTurns), nil
}

// parallelSafe tools are read-only: concurrent execution cannot corrupt
// the worktree. Everything else runs sequentially in call order.
var parallelSafe = map[string]bool{"read_file": true, "list_dir": true}

type turnResult struct {
	id          string
	modelResult string
	logLine     string
}

// executeTurn runs one turn's tool calls: all-parallel when every call
// is read-only, sequential otherwise. Results return in call order, and
// transcript lines are emitted in call order too.
func (l *LoopAdapter) executeTurn(ctx context.Context, reg *Registry, workspace string, calls []toolCall, turn func(string, ...any)) []turnResult {
	allSafe := len(calls) > 0
	for _, tc := range calls {
		if !parallelSafe[tc.Function.Name] {
			allSafe = false
			break
		}
	}
	results := make([]turnResult, len(calls))
	if !allSafe {
		for i, tc := range calls {
			results[i] = runOne(ctx, reg, workspace, tc)
			turn("  %s", results[i].logLine)
		}
		return results
	}
	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = runOne(ctx, reg, workspace, tc)
		}()
	}
	wg.Wait()
	for _, r := range results {
		turn("  %s", r.logLine)
	}
	return results
}

func runOne(ctx context.Context, reg *Registry, workspace string, tc toolCall) turnResult {
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		args = map[string]any{}
	}
	out, err := reg.Execute(ctx, workspace, tc.Function.Name, args)
	result := out
	if err != nil {
		result = "ERROR: " + err.Error()
	}
	if strings.TrimSpace(result) == "" {
		result = "(empty result)"
	}
	// The transcript log keeps everything; the model only gets
	// the tail. Full command output in-context burns the window
	// and buries the signal (a `cargo test` dump ended a run).
	modelResult := result
	if len(modelResult) > 2000 {
		modelResult = "...[earlier output in transcript]...\n" + modelResult[len(modelResult)-2000:]
	}
	line := fmt.Sprintf("%s -> %s", tc.Function.Name, truncate(strings.TrimSpace(result), 300))
	return turnResult{id: tc.ID, modelResult: modelResult, logLine: line}
}
func done(id, taskID, workspace string, started time.Time, stdout, stderr string, code int, format string, args ...any) *agent.Execution {
	if format != "" {
		msg := fmt.Sprintf(format, args...)
		if stderr == "" {
			stderr = msg
		} else {
			stderr += "\n" + msg
		}
	}
	return &agent.Execution{
		ID: id, TaskID: taskID, AgentID: "loop", Workspace: workspace,
		Stdout: stdout, Stderr: stderr, ExitCode: code,
		StartedAt: started, EndedAt: time.Now().UTC(),
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " | ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// isTransient reports failures worth retrying: network/timeout class
// plus rate limits (parallel fleets hit 429s; backoff absorbs them).
// API rejections (auth, schema) fail fast instead.
func isTransient(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, k := range []string{"transport:", "timeout", "deadline exceeded", "connection reset", "connection refused", "temporary failure", "eof", "429", "rate limit", "too many requests", "overloaded", "try again"} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}
