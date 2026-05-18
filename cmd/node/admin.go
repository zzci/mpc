package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/zzci/mpc/internal/server/admin"
	"github.com/zzci/mpc/internal/server/coorddb"
)

// admin-api wiring (A-001). It runs in the coord process (admin.md §5:
// admin-api 随 coord 进程 coord.enable) over the same D-001 store, so the
// administrator can unlock the LOCKED store at runtime — coord cannot unlock
// itself (server.md C9b: passphrase never in config/env/KMS, only via the
// interactive admin-api).
//
// server.md's config chapter has no admin fields and internal/node is out of
// this task's scope, so admin settings are read directly from TSSNODE_ADMIN__*
// environment variables — the same env-injection channel server.md's "密钥处理"
// approves for secrets, and the same local-env-override precedent already used
// in coord.go for TSSNODE_COORD__EXTERNAL__CALLBACK_URL. Tokens are secrets:
// they are injected via env only, never a committed literal.
const (
	envAdminListen       = "TSSNODE_ADMIN__LISTEN"
	envAdminReadToken    = "TSSNODE_ADMIN__READ_TOKEN"
	envAdminControlToken = "TSSNODE_ADMIN__CONTROL_TOKEN"
	defaultAdminListen   = "127.0.0.1:9091" // admin.md §5: NOT public
)

// startAdmin launches the admin-api in a goroutine over the shared store and
// returns a stop function. When the read/control tokens are not configured the
// admin-api is not started (no insecure default): coord still runs, the store
// stays LOCKED by design and the A/B API fail-closes 503 until an operator
// configures the tokens and unlocks.
func startAdmin(ctx context.Context, store *coorddb.Store, logger *slog.Logger) (stop func()) {
	readTok := os.Getenv(envAdminReadToken)
	ctrlTok := os.Getenv(envAdminControlToken)
	if readTok == "" || ctrlTok == "" {
		logger.Warn("admin-api disabled: " + envAdminReadToken + " / " + envAdminControlToken +
			" not set; coord store stays LOCKED (API fail-closes 503) until configured")
		return func() {}
	}

	cfg := admin.Config{
		Listen:       orDefault(os.Getenv(envAdminListen), defaultAdminListen),
		ReadToken:    readTok,
		ControlToken: ctrlTok,
	}
	srv, err := admin.New(cfg, store, admin.WithLogger(logger))
	if err != nil {
		logger.Warn("admin-api disabled: invalid config", "err", err.Error())
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if serr := srv.Start(ctx); serr != nil {
			logger.Error("admin-api stopped with error", "err", serr.Error())
		}
	}()
	return func() {
		srv.Stop()
		<-done
	}
}
