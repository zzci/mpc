package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/zzci/mpc/internal/server"
	"github.com/zzci/mpc/internal/server/coord"
	"github.com/zzci/mpc/internal/server/coorddb"
)

// runCoord wires the real coord role (X-001). It builds the D-001 encrypted
// store + in-memory presence, derives the coord runtime config from
// server.CoordConfig, then serves the A/B/groups API. The store starts LOCKED by
// design (server.md C9b): the API fail-closes 503 until an UnlockProvider
// (admin-api, A-001) supplies the in-memory passphrase — never from
// config/env/KMS. coord never imports internal/server/relay.
//
// runCoord blocks until ctx is done (the shared SIGINT/SIGTERM context owned by
// main.go), then shuts the API + admin-api down gracefully. In the relay+coord
// dual-role deployment this runs concurrently with runRelay on the same ctx
// (FIX-002).
func runCoord(ctx context.Context, cfg server.Config) error {
	cc := cfg.Coord

	// Config framework v2 (user ruling 2026-05-19): every value may be a
	// literal or an env:/file: reference; server.Config.Validate has
	// already fail-fasted any missing required value before run() reaches
	// here. external auth is fixed api_key, result delivery fixed webhook,
	// notification a single fixed webhook — all three are required.
	dbPath, err := resolveRef(cc.DB.DSN)
	if err != nil {
		return fmt.Errorf("coord db dsn: %w", err)
	}
	apiKey, err := resolveRef(cc.External.APIKey)
	if err != nil {
		return fmt.Errorf("coord external.api_key: %w", err)
	}
	resultWebhook, err := resolveRef(cc.External.ResultWebhook)
	if err != nil {
		return fmt.Errorf("coord external.result_webhook: %w", err)
	}
	notifyWebhook, err := resolveRef(cc.Notify.Webhook)
	if err != nil {
		return fmt.Errorf("coord notify.webhook: %w", err)
	}

	skew, err := time.ParseDuration(orDefault(cc.TTL.SkewTolerance, "30s"))
	if err != nil {
		return fmt.Errorf("coord ttl.skew_tolerance: %w", err)
	}
	dispatch, err := time.ParseDuration(orDefault(cc.Dispatch.Timeout, "120s"))
	if err != nil {
		return fmt.Errorf("coord dispatch.timeout: %w", err)
	}

	ccfg := coord.Config{
		Listen:          orDefault(cc.HTTP.Listen, ":8080"),
		DBPath:          dbPath,
		APIKey:          apiKey,
		CallbackURL:     resultWebhook,
		NotifyWebhook:   notifyWebhook,
		SkewTolerance:   skew,
		SignerSelect:    orDefault(cc.Quorum.SignerSelect, "liveness"),
		DispatchTimeout: dispatch,
	}

	// database.md §7.1: encryption.enable defaults true (encrypted + LOCKED,
	// admin-api unlock). dev/test may set it false — the production iron-law
	// guardrail in server.Config.Validate has already fail-closed any
	// unconfirmed disable before we reach here, so an encryptDisabled path is
	// a confirmed non-production one.
	encryptDisabled := !cc.DB.Encryption.Enable
	var store *coorddb.Store
	if encryptDisabled {
		store = coorddb.NewPlaintextStore(dbPath)
	} else {
		store = coorddb.NewStore(dbPath)
	}
	defer func() { _ = store.Close() }()

	presence, err := coorddb.NewPresence(90*time.Second, 30*time.Second)
	if err != nil {
		return fmt.Errorf("coord presence: %w", err)
	}
	defer func() { _ = presence.Close() }()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	// Notification is a single fixed webhook (user ruling 2026-05-19):
	// coord POSTs dispatch-wake events here and an external channel
	// translates/delivers them (FCM/APNS/etc.). coord holds no push
	// credentials and never blocks on delivery.
	co, err := coord.New(ccfg, store, presence,
		coord.WithLogger(logger),
		coord.WithNotifier(newWebhookNotifier(notifyWebhook, logger)))
	if err != nil {
		return fmt.Errorf("coord init: %w", err)
	}

	// admin-api (A-001) runs in this coord process over the same store and
	// owns the interactive unlock; bring it up before the blocking coord
	// serve so an operator can unlock the LOCKED store at runtime.
	stopAdmin := startAdmin(ctx, store, logger)
	defer stopAdmin()

	if encryptDisabled {
		// dev/test whole-DB encryption disabled (database.md §7.1): mount
		// the unencrypted DB directly; coord starts UNLOCKED-equivalent and
		// data endpoints are immediately ready — E2E runs the full ring on this.
		if err := store.OpenInsecure(ctx); err != nil {
			return fmt.Errorf("coord open (encryption disabled): %w", err)
		}
		logger.Warn("coord whole-DB encryption DISABLED (dev/test only, ALLOW_INSECURE_DB confirmed); .db is plaintext at rest — never use this artifact in production")
	} else {
		// The unlock passphrase is supplied by the admin-api (A-001) at
		// runtime; until then no UnlockProvider exists and the store stays
		// LOCKED — the designed startup state (server.md C9b), API
		// fail-closes 503.
		if err := co.Unlock(ctx, unlockProvider(), 5); err != nil {
			logger.Warn("coord store LOCKED (no unlock provider yet; API fail-closes 503 until admin-api unlock)",
				"reason", err.Error())
		}
	}

	logger.Info("coord serving", "listen", ccfg.Listen)
	return co.Start(ctx)
}

// unlockProvider stays nil by design: A-001's admin-api drives unlock/relock
// directly on the shared coorddb.Store (its own rate-limited, audited unlock
// endpoint) rather than feeding coord's one-shot startup loop, so coord's
// initial Unlock is a no-op (errUnlockUnavailable) and the store stays LOCKED
// until the operator unlocks via admin-api — exactly the designed startup
// state (server.md C9b).
func unlockProvider() coord.UnlockProvider { return nil }

// resolveRef resolves a config value the same way server.Config does
// (config framework v2, user ruling 2026-05-19): env:/file: are resolved
// references, any other non-empty string is a literal returned as-is, and
// an empty value yields "" (coord.Config.validate then rejects a required
// empty). Plaintext literals are no longer rejected.
func resolveRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return "", nil
	case strings.HasPrefix(ref, "env:"):
		return os.Getenv(strings.TrimPrefix(ref, "env:")), nil
	case strings.HasPrefix(ref, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(ref, "file:"))
		if err != nil {
			return "", fmt.Errorf("read value file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	default:
		return ref, nil
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
