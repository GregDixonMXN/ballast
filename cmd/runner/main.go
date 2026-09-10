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
	cli := &runner.Client{Base: *server, Token: token}
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
