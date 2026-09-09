// Command runner is the execution-node daemon: registers with the control
// plane, heartbeats, polls for assigned work (outbound-only: no inbound
// ports on dev machines), manages worktrees, spawns agents, runs tests,
// and reports file changes. MVP runs co-located; the protocol is remote-ready.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"runtime"
	"time"

	"ballast/internal/telemetry"
	"ballast/internal/workspace"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "control plane URL")
	root := flag.String("root", "./.ballast-worktrees", "worktree root")
	name := flag.String("name", "", "runner hostname override")
	flag.Parse()

	telemetry.Init("ballast-runner")
	host := *name
	if host == "" {
		host, _ = os.Hostname()
	}
	log.Printf("runner %s (%s/%s) → %s", host, runtime.GOOS, runtime.GOARCH, *server)
	_ = workspace.NewManager(*root)

	// MVP heartbeat loop: proves liveness + capability reporting.
	// Work dispatch polling lands here next (GET /runners/{id}/work).
	ctx := context.Background()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		telemetry.Log(ctx, "runner heartbeat", "host", host)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
