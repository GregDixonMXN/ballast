// Command server boots the Ballast control plane: record store
// (Postgres when DATABASE_URL is set, durable local JSON otherwise), event bus,
// services, HTTP API (REST + SSE). Stateless except the store + Git.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ballast/internal/api"
	"ballast/internal/auth"
	"ballast/internal/events"
	overseerPkg "ballast/internal/overseer"
	"ballast/internal/services"
	"ballast/internal/store"
	"ballast/internal/telemetry"
	"ballast/internal/workspace"
)

var version = "1.0.0-rc.1"

func main() {
	showVersion := flag.Bool("version", false, "print version")
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	wtRoot := flag.String("worktrees", "./.ballast-worktrees", "worktree root")
	tokenFile := flag.String("token-file", "./.ballast/operator.token", "private persistent operator token file")
	dataFile := flag.String("data", "./.ballast/state.json", "durable local store (used without DATABASE_URL)")
	flag.Parse()
	if *showVersion {
		fmt.Println("ballast-server " + version)
		return
	}

	telemetry.Init("ballast-server")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		repo     services.Repo
		eventSto events.Store
		mode     string
	)
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		db, err := store.Open(ctx, dsn)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		defer db.SQL.Close()
		repo = store.NewPGRepo(db.SQL)
		eventSto = store.NewPGEventStore(db.SQL)
		mode = "postgres (persistent)"
	} else {
		local, err := store.OpenLocal(*dataFile)
		if err != nil {
			log.Fatalf("local store: %v", err)
		}
		defer local.Close()
		repo = local
		eventSto = local
		mode = "local (persistent)"
	}

	bus := events.NewMemoryBus(eventSto)
	toks := auth.NewTokens()
	if err := toks.LoadOperator(*tokenFile); err != nil {
		log.Fatalf("operator credential: %v", err)
	}

	mgr := workspace.NewManager(*wtRoot)
	if err := services.Recover(ctx, repo, mgr); err != nil {
		log.Fatalf("recovery: %v", err)
	}
	srv := api.New(bus, toks)
	srv.EventStore = eventSto
	srv.Projects = &services.Projects{Repo: repo}
	srv.Tasks = &services.Tasks{Repo: repo, Mgr: mgr}
	srv.Workspaces = &services.Workspaces{Repo: repo, Mgr: mgr}
	srv.Changesets = &services.Changesets{Repo: repo}

	// Overseer: structural supervision, harness-blind. Every minute it
	// scans projects for duplicate tasks, file collisions across live
	// workspaces, stuck runs, and overlapping claims, posting each new
	// finding once to the project board. Advisory only.
	projectsSvc := &services.Projects{Repo: repo}
	tasksSvc := &services.Tasks{Repo: repo, Mgr: mgr}
	wsSvc := &services.Workspaces{Repo: repo, Mgr: mgr}
	overseer := overseerPkg.New()
	go func() {
		tick := time.NewTicker(60 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				sweepAll(ctx, projectsSvc, tasksSvc, wsSvc, srv, overseer)
			}
		}
	}()

	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	log.Printf("ballast server on %s [%s] (pid %d)", *addr, mode, os.Getpid())
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
	}()
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// sweepAll runs one overseer pass over every project: tasks, live
// workspace files, and active claims feed the sweep; new findings land
// on the project board. Errors are logged, never fatal: supervision
// must not take down the control plane.
func sweepAll(ctx context.Context, projects *services.Projects, tasks *services.Tasks, ws *services.Workspaces, srv *api.Server, o *overseerPkg.Overseer) {
	_ = ctx
	plist, err := projects.List()
	if err != nil {
		log.Printf("overseer: list projects: %v", err)
		return
	}
	for _, p := range plist {
		tasksAny, err := tasks.List(p.ID)
		if err != nil {
			continue
		}
		wsAny, err := ws.List(p.ID)
		if err != nil {
			continue
		}
		files := map[string][]string{}
		for _, wv := range overseerPkg.AdaptWS(wsAny, nil) {
			if wv.Status != "RUNNING" {
				continue
			}
			if changed, err := ws.ChangedFiles(wv.ID); err == nil {
				files[wv.ID] = changed
			}
		}
		findings := o.Sweep(p.ID, overseerPkg.AdaptTasks(tasksAny), overseerPkg.AdaptWS(wsAny, files))
		if srv.Leases != nil {
			findings = append(findings, o.CheckClaims(srv.Leases.Active(p.ID))...)
		}
		for _, f := range findings {
			log.Printf("overseer [%s]: %s", p.ID[:8], f)
		}
		overseerPkg.Post(srv.Notes, p.ID, findings)
	}
}
