// Command runner is the execution-node daemon: registers with the control
// plane, heartbeats, polls for assigned work (outbound-only: no inbound
// ports on dev machines), runs the adapter inside the assigned worktree,
// optionally runs the work item's test command, and reports the outcome.
// The server owns every state transition; the runner only reports facts.
package main

import (
	"ballast/internal/auth"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"ballast/internal/agent"
	"ballast/internal/agentloop"
	"ballast/internal/api"
	"ballast/internal/runner"
	"ballast/internal/telemetry"
)

var version = "1.0.0-rc.1"

func main() {
	showVersion := flag.Bool("version", false, "print version")
	server := flag.String("server", "http://localhost:8080", "control plane URL")
	tokenFile := flag.String("token-file", "./.ballast/operator.token", "private operator token file used only for registration")
	root := flag.String("worktrees", "./.ballast-worktrees", "allowed co-located worktree root")
	timeout := flag.Duration("task-timeout", 30*time.Minute, "maximum agent and test duration")
	name := flag.String("name", "", "runner hostname override")
	agentBin := flag.String("agent", os.Getenv("AGENT_BIN"), "custom agent binary (extra adapter)")
	agentHome := flag.String("agent-home", "", "explicit dedicated agent login/config home (read/write by agents); default disposable unauthenticated home")
	modelKeyFile := flag.String("model-key-file", "", "API key file enabling the in-process tool-loop adapter (reads key once at startup, never logged)")
	modelName := flag.String("model", "muse-spark-1.3", "model id for the tool-loop adapter")
	modelBaseURL := flag.String("model-base-url", "https://api.meta.ai/v1", "OpenAI-compatible base URL for the tool-loop adapter")
	once := flag.Bool("once", false, "poll once and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("ballast-runner " + version)
		return
	}

	telemetry.Init("ballast-runner")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	host := *name
	if host == "" {
		host, _ = os.Hostname()
	}
	token, err := auth.ReadCredential(*tokenFile)
	if err != nil {
		log.Fatalf("credential: %v", err)
	}
	adapters := agent.Registry(nil)
	if *agentBin != "" {
		adapters = append(adapters, agent.NewShell("custom", *agentBin, nil))
	}
	if *agentHome != "" {
		absolute, err := filepath.Abs(*agentHome)
		if err != nil {
			log.Fatal(err)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			log.Fatalf("agent home: %v", err)
		}
		if !info.IsDir() {
			log.Fatal("agent home must be an existing dedicated directory")
		}
		*agentHome = absolute
	}
	capabilities := []string{"git", "tests"}
	for _, adapter := range adapters {
		if shell, ok := adapter.(*agent.ShellAdapter); ok {
			shell.Home = *agentHome
		}
		if adapter.Available(ctx) {
			capabilities = append(capabilities, adapter.Name())
		}
	}
	if *modelKeyFile != "" {
		key, err := readAPIKeyFile(*modelKeyFile)
		if err != nil {
			log.Fatalf("model key: %v", err)
		}
		adapters = append(adapters, agentloop.NewLoop("loop", agentloop.LoopConfig{
			Model: agentloop.Config{BaseURL: *modelBaseURL, APIKey: key, Model: *modelName},
			OnTurn: func(s string) {
				log.Printf("loop: %s", s)
			},
		}))
	}
	cli := &runner.Client{Base: *server, Token: token}
	for _, adapter := range adapters {
		if loopAd, ok := adapter.(*agentloop.LoopAdapter); ok {
			loopAd := loopAd
			loopAd.SetHooks(
				func() []string {
					if _, project := loopAd.Scope(); project != "" {
						return cli.Board(project)
					}
					return nil
				},
				func(text string) error {
					ws, project := loopAd.Scope()
					if ws == "" || project == "" {
						return fmt.Errorf("no bound work")
					}
					return cli.PostNote(project, ws, text)
				},
				func(pattern string) []string {
					if ws, _ := loopAd.Scope(); ws != "" {
						return cli.Claim(ws, pattern)
					}
					return nil
				},
			)
		}
	}
	id, runnerTok, err := cli.Register(api.RunnerInfo{
		Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Capabilities: capabilities,
		HasGit:       true,
	})
	if err != nil {
		log.Fatalf("register: %v", err)
	}
	cli.Token = runnerTok // run on the scoped runner token from here on
	log.Printf("runner %s (%s/%s) registered", id, runtime.GOOS, runtime.GOARCH)

	ex := &runner.Executor{Client: cli, Adapters: adapters, WorktreeRoot: *root, Timeout: *timeout}

	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	poll := time.NewTicker(5 * time.Second)
	defer poll.Stop()

	doPoll := func() bool {
		item, ok, err := cli.Poll(id)
		if err != nil {
			if strings.Contains(err.Error(), ": 401") {
				fresh, e := auth.ReadCredential(*tokenFile)
				if e == nil {
					cli.Token = fresh
					next, tok, e := cli.Register(api.RunnerInfo{Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH, HasGit: true, Capabilities: capabilities})
					if e == nil {
						id = next
						cli.Token = tok
						log.Printf("runner re-registered after control-plane restart")
					} else {
						cli.Token = ""
					}
				}
			}
			log.Printf("poll: %v", err)
			return false
		}
		if !ok {
			return false
		}
		telemetry.Log(ctx, "work received", "workspace", item.WorkspaceID, "task", item.TaskID)
		beatCtx, cancelBeat := context.WithCancel(ctx)
		beatClient := &runner.Client{Base: cli.Base, Token: cli.Token}
		beatID := id
		go func() {
			tick := time.NewTicker(15 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-beatCtx.Done():
					return
				case <-tick.C:
					_ = beatClient.Heartbeat(beatID)
				}
			}
		}()
		defer cancelBeat()
		if _, err := ex.RunOnce(ctx, id, func() (api.WorkItem, bool, error) {
			return item, true, nil
		}); err != nil {
			log.Printf("work: %v", err)
		}
		return true
	}

	if *once {
		doPoll()
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-beat.C:
			if err := cli.Heartbeat(id); err != nil {
				log.Printf("heartbeat: %v", err)
			}
		case <-poll.C:
			doPoll()
		}
	}
}

// readAPIKeyFile reads an opaque model API key: 0600 regular file,
// trimmed, length-checked. Unlike auth.ReadCredential it accepts any
// key format — model keys are not hex operator tokens.
func readAPIKeyFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("key file must be a private regular file (0600)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(raw))
	if len(key) < 16 || len(key) > 4096 {
		return "", fmt.Errorf("key file holds no plausible API key")
	}
	return key, nil
}
