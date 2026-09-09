// Command runner is the execution-node daemon: registers with the control
// plane, heartbeats, polls for assigned work (outbound-only: no inbound
// ports on dev machines), runs the adapter inside the assigned worktree,
// optionally runs the work item's test command, and reports the outcome.
// The server owns every state transition; the runner only reports facts.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"runtime"
	"time"

	"ballast/internal/agent"
	"ballast/internal/api"
	"ballast/internal/runner"
	"ballast/internal/telemetry"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "control plane URL")
	token := flag.String("token", os.Getenv("BALLAST_TOKEN"), "user token for registration")
	name := flag.String("name", "", "runner hostname override")
	agentBin := flag.String("agent", os.Getenv("AGENT_BIN"), "custom agent binary (extra adapter)")
	once := flag.Bool("once", false, "poll once and exit")
	flag.Parse()

	telemetry.Init("ballast-runner")
	ctx := context.Background()

	host := *name
	if host == "" {
		host, _ = os.Hostname()
	}
	cli := &runner.Client{Base: *server, Token: *token}
	id, runnerTok, err := cli.Register(api.RunnerInfo{
		Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Capabilities: []string{"git", "tests"},
		HasGit:       true,
	})
	if err != nil {
		log.Fatalf("register: %v", err)
	}
	cli.Token = runnerTok // run on the scoped runner token from here on
	log.Printf("runner %s (%s/%s) registered", id, runtime.GOOS, runtime.GOARCH)

	adapters := agent.Registry(nil)
	if *agentBin != "" {
		adapters = append(adapters, agent.NewShell("custom", *agentBin, nil))
	}
	ex := &runner.Executor{Client: cli, Adapters: adapters}

	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	poll := time.NewTicker(5 * time.Second)
	defer poll.Stop()

	doPoll := func() bool {
		item, ok, err := cli.Poll(id)
		if err != nil {
			log.Printf("poll: %v", err)
			return false
		}
		if !ok {
			return false
		}
		telemetry.Log(ctx, "work received", "workspace", item.WorkspaceID, "task", item.TaskID)
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
