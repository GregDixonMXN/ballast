// Package agentloop is Ballast's in-process agent runtime, extracted from
// Reeve's tool registry + orchestrator loop shape without the Wails app.
//
// A LoopAdapter drives a model over an OpenAI-compatible chat-completions
// endpoint directly and executes tool calls inside the worktree itself.
// Every turn is recorded in the returned Execution transcript, so a dead
// run always leaves evidence instead of an empty BLOCKED task.
package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config for the model endpoint. APIKey is never logged.
type Config struct {
	BaseURL string // e.g. https://api.meta.ai/v1
	APIKey  string
	Model   string // e.g. muse-spark-1.3
	HTTP    *http.Client
}

func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = "https://api.meta.ai/v1"
	}
	if c.Model == "" {
		c.Model = "muse-spark-1.3"
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 120 * time.Second}
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	return c
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	ToolChoice  string        `json:"tool_choice,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
		Finish  string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// FinishOf returns the first choice's finish reason, if any.
func (r chatResponse) FinishOf() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Finish
}

// Reply is one model turn: the message plus its finish reason.
type Reply struct {
	Message chatMessage
	Finish  string
}

// Client speaks OpenAI-compatible chat completions with function tools.
type Client struct {
	cfg Config
}

func NewClient(cfg Config) *Client { return &Client{cfg: cfg.withDefaults()} }

// Complete sends the conversation and returns the assistant's message.
// Transport errors and API error bodies are both Go errors with the key
// redacted (the key never appears in requests bodies we log).
func (c *Client) Complete(ctx context.Context, msgs []chatMessage, tools []chatTool) (Reply, error) {
	body, err := json.Marshal(chatRequest{
		Model:       c.cfg.Model,
		Messages:    msgs,
		Tools:       tools,
		ToolChoice:  "auto",
		MaxTokens:   16384,
		Temperature: 0.2,
	})
	if err != nil {
		return Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	res, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return Reply{}, fmt.Errorf("model transport: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return Reply{}, err
	}
	if res.StatusCode >= 400 {
		return Reply{}, fmt.Errorf("model HTTP %d: %s", res.StatusCode, firstLine(raw))
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Reply{}, fmt.Errorf("model decode: %w", err)
	}
	if out.Error != nil {
		return Reply{}, fmt.Errorf("model API: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return Reply{}, fmt.Errorf("model returned no choices")
	}
	return Reply{Message: out.Choices[0].Message, Finish: out.FinishOf()}, nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
