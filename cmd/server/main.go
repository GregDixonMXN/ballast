// Command server boots the Ballast control plane: record store
// (Postgres when DATABASE_URL is set, memory otherwise), event bus,
// services, HTTP API (REST + SSE). Stateless except the store + Git.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"ballast/internal/api"
	"ballast/internal/auth"
	"ballast/internal/events"
	"ballast/internal/services"
	"ballast/internal/store"
	"ballast/internal/telemetry"
	"ballast/internal/workspace"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	wtRoot := flag.String("worktrees", "./.ballast-worktrees", "worktree root")
	flag.Parse()

	telemetry.Init("ballast-server")
	ctx := context.Background()

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
		repo = store.NewMemoryRepo()
		eventSto = events.NewMemoryStore()
		mode = "memory (ephemeral — set DATABASE_URL for persistence)"
	}

	bus := events.NewMemoryBus(eventSto)
	toks := auth.NewTokens()
	// Dev convenience token printed at boot; OIDC replaces this in prod.
	dev := toks.Mint(auth.Identity{ID: "dev", Kind: "human", Roles: []string{"admin"}})
	log.Printf("dev token: %s", dev)

	mgr := workspace.NewManager(*wtRoot)
	srv := api.New(bus, toks)
	srv.Projects = &services.Projects{Repo: repo}
	srv.Tasks = &services.Tasks{Repo: repo}
	srv.Workspaces = &services.Workspaces{Repo: repo, Mgr: mgr}
	srv.Changesets = &services.Changesets{Repo: repo}

	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second}
	log.Printf("ballast server on %s [%s] (pid %d)", *addr, mode, os.Getpid())
	log.Fatal(httpSrv.ListenAndServe())
}
