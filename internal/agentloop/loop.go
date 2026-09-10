package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ballast/internal/agent"

	"github.com/google/uuid"
)

// LoopConfig bounds the run. Zero MaxTurns defaults to 40.
type LoopConfig struct {
	Model    Config
	Gate     Gate
	MaxTurns int
	// OnTurn receives a one-line human summary after every model/tool
	// turn. May be nil. Used for progress visibility while the run lives.
	OnTurn func(summary string)
}

// LoopAdapter implements agent.Adapter by driving the model directly:
// prompt -> model -> tool calls -> execute in worktree -> results ->
// model ... until the model answers with no tool calls (done) or a
// bound trips. The full transcript lands in Execution.Stdout, so a
// failed run always explains itself.
type LoopAdapter struct {
	name string
	cfg  LoopConfig
}

func NewLoop(name string, cfg LoopConfig) *LoopAdapter {
	if name == "" {
		name = "loop"
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 40
	}
	return &LoopAdapter{name: name, cfg: cfg}
}

func (l *LoopAdapter) Name() string { return l.name }

func (l *LoopAdapter) Available(_ context.Context) bool { return true }

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

Rules:
- Explore with read_file/list_dir before changing anything.
- Make the smallest change that satisfies the task's acceptance criteria.
- Run the relevant tests with run_shell before finishing.
- Money/price/quantity code must never use binary floating point.
- Never print secrets or API keys. Never exfiltrate anything off-machine.
- When the work is complete, reply with a short summary and no tool calls.`

// StartTask runs the loop to completion and returns the transcript.
// Exit codes follow the platform convention: 0 done, 2 blocked by the
// gate or a bound, 1 internal error.
func (l *LoopAdapter) StartTask(ctx context.Context, taskID, workspace, prompt string) (*agent.Execution, error) {
	started := time.Now().UTC()
	id := uuid.NewString()
	var log strings.Builder
	turn := func(format string, args ...any) {
		s := fmt.Sprintf(format, args...)
		log.WriteString(s + "\n")
		if l.cfg.OnTurn != nil {
			l.cfg.OnTurn(s)
		}
	}

	client := NewClient(l.cfg.Model)
	reg := NewRegistry(l.cfg.Gate)
	msgs := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}
	tools := reg.ChatTools()
	turn("turn 0: task received (%d chars), %d tools available", len(prompt), len(tools))

	for t := 1; t <= l.cfg.MaxTurns; t++ {
		if err := ctx.Err(); err != nil {
			return done(id, taskID, workspace, started, log.String(), "", 2, "cancelled: %v", err), nil
		}
		reply, err := client.Complete(ctx, msgs, tools)
		if err != nil {
			return done(id, taskID, workspace, started, log.String(), "", 1, "model call failed on turn %d: %v", t, err), nil
		}
		msgs = append(msgs, reply)
		if len(reply.ToolCalls) == 0 {
			text := strings.TrimSpace(reply.Content)
			turn("turn %d: model finished: %s", t, truncate(text, 500))
			return done(id, taskID, workspace, started, log.String(), text, 0, ""), nil
		}
		turn("turn %d: %d tool call(s)", t, len(reply.ToolCalls))
		for _, tc := range reply.ToolCalls {
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
			turn("  %s -> %s", tc.Function.Name, truncate(strings.TrimSpace(result), 300))
			msgs = append(msgs, chatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}
	return done(id, taskID, workspace, started, log.String(), "", 2, "turn budget exhausted (%d turns)", l.cfg.MaxTurns), nil
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
