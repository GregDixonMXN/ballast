// Command server boots the Ballast control plane: migrations, event bus,
// services, HTTP API (REST + SSE). Stateless except Postgres + Git.
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
	"ballast/internal/telemetry"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	telemetry.Init("ballast-server")
	ctx := context.Background()

	bus := events.NewMemoryBus(events.NewMemoryStore())
	toks := auth.NewTokens()
	// Dev convenience token printed at boot; OIDC replaces this in prod.
	dev := toks.Mint(auth.Identity{ID: "dev", Kind: "human", Roles: []string{"admin"}})
	log.Printf("dev token: %s", dev)

	srv := api.New(bus, toks)
	_ = ctx
	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second}
	log.Printf("ballast server on %s (pid %d)", *addr, os.Getpid())
	log.Fatal(httpSrv.ListenAndServe())
}
