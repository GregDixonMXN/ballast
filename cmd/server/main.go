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
