package agentloop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ToolFunc executes one tool call. Args are the parsed JSON arguments
// object; the returned string goes back to the model verbatim.
type ToolFunc func(ctx context.Context, workdir string, args map[string]any) (string, error)

// Gate approves or denies a call before execution (paldron-shaped hook).
// A nil gate allows everything; denials are reported to the model.
type Gate func(name string, args map[string]any) error

type definition struct {
	Name        string
	Description string
	Parameters  map[string]any
	Fn          ToolFunc
}

// Registry is the agent's hands: a fixed tool set executed inside one
// worktree, each call recorded by the loop. Mirrors Reeve's registry
// boundary rule — the gate runs before execution, never after.
type Registry struct {
	tools map[string]definition
	gate  Gate
}

func NewRegistry(gate Gate) *Registry {
	r := &Registry{tools: map[string]definition{}, gate: gate}
	r.add("read_file", "Read a file's full content. Path may be absolute or relative to the worktree.",
		obj(map[string]any{"path": str("File path to read")}, "path"),
		func(ctx context.Context, wd string, args map[string]any) (string, error) {
			p, err := resolve(wd, stringArg(args, "path"))
			if err != nil {
				return "", err
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			if len(raw) > 200000 {
				return "", fmt.Errorf("file too large (%d bytes), use run_shell to inspect", len(raw))
			}
			return string(raw), nil
		})
	r.add("write_file", "Write (create or overwrite) a file with complete content.",
		obj(map[string]any{
			"path":    str("File path to write (created with parents as needed)"),
			"content": str("Complete file content"),
		}, "path", "content"),
		func(ctx context.Context, wd string, args map[string]any) (string, error) {
			p, err := resolve(wd, stringArg(args, "path"))
			if err != nil {
				return "", err
			}
			c := stringArg(args, "content")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(c), p), nil
		})
	r.add("edit_file", "Replace the first occurrence of old_text with new_text in a file.",
		obj(map[string]any{
			"path":     str("File path to edit"),
			"old_text": str("Exact text to find (must be unique in file)"),
			"new_text": str("Replacement text"),
		}, "path", "old_text", "new_text"),
		func(ctx context.Context, wd string, args map[string]any) (string, error) {
			p, err := resolve(wd, stringArg(args, "path"))
			if err != nil {
				return "", err
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			old, new := stringArg(args, "old_text"), stringArg(args, "new_text")
			if n := strings.Count(string(raw), old); n != 1 {
				return "", fmt.Errorf("old_text found %d times, need exactly 1", n)
			}
			out := strings.Replace(string(raw), old, new, 1)
			if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s", p), nil
		})
	r.add("list_dir", "List directory entries (names only).",
		obj(map[string]any{"path": str("Directory path, default worktree root")}),
		func(ctx context.Context, wd string, args map[string]any) (string, error) {
			rel := stringArg(args, "path")
			if rel == "" {
				rel = "."
			}
			p, err := resolve(wd, rel)
			if err != nil {
				return "", err
			}
			ents, err := os.ReadDir(p)
			if err != nil {
				return "", err
			}
			names := make([]string, 0, len(ents))
			for _, e := range ents {
				n := e.Name()
				if e.IsDir() {
					n += "/"
				}
				names = append(names, n)
			}
			sort.Strings(names)
			return strings.Join(names, "\n"), nil
		})
	r.add("run_shell", "Run a command (no shell interpolation: argv split on spaces) in the worktree. For builds, tests, grep-like inspection. Timeout 120s, output capped.",
		obj(map[string]any{"command": str("Command line, e.g. \"cargo test --workspace\"")}, "command"),
		func(ctx context.Context, wd string, args map[string]any) (string, error) {
			fields := strings.Fields(stringArg(args, "command"))
			if len(fields) == 0 {
				return "", fmt.Errorf("empty command")
			}
			tctx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()
			cmd := exec.CommandContext(tctx, fields[0], fields[1:]...)
			cmd.Dir = wd
			out, err := cmd.CombinedOutput()
			if len(out) > 60000 {
				out = append(out[:60000], []byte("\n...[truncated]")...)
			}
			if err != nil {
				return fmt.Sprintf("exit != 0: %v\n%s", err, out), nil
			}
			return string(out), nil
		})
	return r
}

func (r *Registry) add(name, desc string, params map[string]any, fn ToolFunc) {
	r.tools[name] = definition{Name: name, Description: desc, Parameters: params, Fn: fn}
}

// Execute runs one call after the gate. Unknown tools and gate denials
// are errors the loop reports back to the model as tool results.
func (r *Registry) Execute(ctx context.Context, workdir, name string, args map[string]any) (string, error) {
	d, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if r.gate != nil {
		if err := r.gate(name, args); err != nil {
			return "", fmt.Errorf("gate denied %s: %w", name, err)
		}
	}
	return d.Fn(ctx, workdir, args)
}

// ChatTools renders definitions in OpenAI function-tool format.
func (r *Registry) ChatTools() []chatTool {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]chatTool, 0, len(names))
	for _, n := range names {
		d := r.tools[n]
		out = append(out, chatTool{Type: "function", Function: chatFunction{
			Name: n, Description: d.Description, Parameters: d.Parameters,
		}})
	}
	return out
}

// resolve pins a path inside the worktree. Absolute paths must already
// sit inside it; relative paths are joined. No escapes, fail closed.
func resolve(workdir, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Join(workdir, p)
	}
	rel, err := filepath.Rel(workdir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes worktree: %s", p)
	}
	return abs, nil
}

func stringArg(args map[string]any, k string) string {
	v, _ := args[k].(string)
	return v
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
