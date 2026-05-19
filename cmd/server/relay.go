package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/zzci/mpc/internal/server"
	"github.com/zzci/mpc/internal/server/relay"
)

// runRelay starts the real relay role (N-002): circuit-relay v2 + rendezvous +
// access control, driven solely by cfg.Relay. The relay is coord-independent
// (server.md R5) — no coord config is read and internal/server/relay never
// imports internal/server/coord — so it can be enabled on its own. runRelay
// blocks until ctx is done (the shared SIGINT/SIGTERM context owned by
// main.go), then shuts the relay down gracefully.
//
// The relay+coord dual-role deployment runs this concurrently with runCoord on
// the same ctx (FIX-002); the trust boundary is unchanged — runRelay reads only
// cfg.Relay and internal/server/relay never imports internal/server/coord.
func runRelay(ctx context.Context, cfg server.Config) error {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	r, err := relay.New(cfg, log)
	if err != nil {
		return fmt.Errorf("start relay: %w", err)
	}

	return r.Run(ctx)
}
